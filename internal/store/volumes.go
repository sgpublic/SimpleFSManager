package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/sgpublic/simplefsmanager/internal/apperr"
)

type Volume struct {
	UUID         string
	MountNumber  int
	MountPath    string
	AutoMount    bool
	DeviceSerial string
	PartitionNum int
}

// RegisterVolume assigns a permanent mount number on first management. Numbers
// are never reused, so a volume's /volN path cannot silently change.
func (s *Store) RegisterVolume(ctx context.Context, uuid, serial string, partitionNumber int) (Volume, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Volume{}, false, fmt.Errorf("begin volume registration: %w", err)
	}
	defer tx.Rollback()

	volume, err := scanVolume(tx.QueryRowContext(ctx, `
		SELECT uuid, mount_number, mount_path, auto_mount, device_serial, partition_number
		FROM volumes WHERE uuid = ?`, uuid))
	if err == nil {
		if err := tx.Commit(); err != nil {
			return Volume{}, false, fmt.Errorf("commit existing volume: %w", err)
		}
		return volume, false, nil
	}
	if err != sql.ErrNoRows {
		return Volume{}, false, fmt.Errorf("query managed volume: %w", err)
	}

	var mountNumber int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(mount_number), 0) + 1 FROM volumes`).Scan(&mountNumber); err != nil {
		return Volume{}, false, fmt.Errorf("allocate mount number: %w", err)
	}
	volume = Volume{
		UUID:         uuid,
		MountNumber:  mountNumber,
		MountPath:    fmt.Sprintf("/vol%d", mountNumber),
		AutoMount:    true,
		DeviceSerial: serial,
		PartitionNum: partitionNumber,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO volumes (uuid, device_serial, partition_number, mount_number, mount_path, auto_mount)
		VALUES (?, ?, ?, ?, ?, 1)`, volume.UUID, volume.DeviceSerial, volume.PartitionNum, volume.MountNumber, volume.MountPath); err != nil {
		return Volume{}, false, fmt.Errorf("insert managed volume: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Volume{}, false, fmt.Errorf("commit volume registration: %w", err)
	}
	return volume, true, nil
}

// ConfigureVolume creates or updates the persistent mount path for a volume.
// When autoMount is nil, an existing volume keeps its current auto-mount flag
// and a newly created volume defaults to auto_mount = false.
func (s *Store) ConfigureVolume(ctx context.Context, uuid, serial string, partitionNumber int, mountPath string, autoMount *bool) (Volume, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Volume{}, fmt.Errorf("begin volume configuration: %w", err)
	}
	defer tx.Rollback()

	volume, err := scanVolume(tx.QueryRowContext(ctx, `
		SELECT uuid, mount_number, mount_path, auto_mount, device_serial, partition_number
		FROM volumes WHERE uuid = ?`, uuid))
	if err == nil {
		var usedByOther bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM volumes WHERE mount_path = ? AND uuid <> ?)`, mountPath, uuid).Scan(&usedByOther); err != nil {
			return Volume{}, fmt.Errorf("check volume mount path: %w", err)
		}
		if usedByOther {
			return Volume{}, apperr.New(http.StatusConflict, apperr.CodeMountPathInUse, fmt.Sprintf("mount path %s is already configured", mountPath))
		}
		nextAutoMount := volume.AutoMount
		if autoMount != nil {
			nextAutoMount = *autoMount
		}
		if _, err := tx.ExecContext(ctx, `UPDATE volumes SET mount_path = ?, auto_mount = ? WHERE uuid = ?`, mountPath, boolToInt(nextAutoMount), uuid); err != nil {
			return Volume{}, fmt.Errorf("update volume mount path: %w", err)
		}
		volume.MountPath = mountPath
		volume.AutoMount = nextAutoMount
		if err := tx.Commit(); err != nil {
			return Volume{}, fmt.Errorf("commit volume configuration: %w", err)
		}
		return volume, nil
	}
	if err != sql.ErrNoRows {
		return Volume{}, fmt.Errorf("query volume configuration: %w", err)
	}

	var mountNumber int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(mount_number), 0) + 1 FROM volumes`).Scan(&mountNumber); err != nil {
		return Volume{}, fmt.Errorf("allocate mount number: %w", err)
	}
	var used bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM volumes WHERE mount_path = ?)`, mountPath).Scan(&used); err != nil {
		return Volume{}, fmt.Errorf("check volume mount path: %w", err)
	}
	if used {
		return Volume{}, apperr.New(http.StatusConflict, apperr.CodeMountPathInUse, fmt.Sprintf("mount path %s is already configured", mountPath))
	}
	initialAutoMount := false
	if autoMount != nil {
		initialAutoMount = *autoMount
	}
	volume = Volume{
		UUID:         uuid,
		MountNumber:  mountNumber,
		MountPath:    mountPath,
		AutoMount:    initialAutoMount,
		DeviceSerial: serial,
		PartitionNum: partitionNumber,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO volumes (uuid, device_serial, partition_number, mount_number, mount_path, auto_mount)
		VALUES (?, ?, ?, ?, ?, ?)`, volume.UUID, volume.DeviceSerial, volume.PartitionNum, volume.MountNumber, volume.MountPath, boolToInt(volume.AutoMount)); err != nil {
		return Volume{}, fmt.Errorf("insert configured volume: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Volume{}, fmt.Errorf("commit volume configuration: %w", err)
	}
	return volume, nil
}

func (s *Store) VolumeByUUID(ctx context.Context, uuid string) (Volume, error) {
	volume, err := scanVolume(s.DB.QueryRowContext(ctx, `
		SELECT uuid, mount_number, mount_path, auto_mount, device_serial, partition_number
		FROM volumes WHERE uuid = ?`, uuid))
	if err != nil {
		if err == sql.ErrNoRows {
			return Volume{}, apperr.New(http.StatusNotFound, apperr.CodeManagedVolumeNotFound, fmt.Sprintf("managed volume %s not found", uuid))
		}
		return Volume{}, fmt.Errorf("query managed volume: %w", err)
	}
	return volume, nil
}

func (s *Store) Volumes(ctx context.Context) ([]Volume, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT uuid, mount_number, mount_path, auto_mount, device_serial, partition_number
		FROM volumes ORDER BY mount_number`)
	if err != nil {
		return nil, fmt.Errorf("list managed volumes: %w", err)
	}
	defer rows.Close()

	volumes := make([]Volume, 0)
	for rows.Next() {
		volume, err := scanVolume(rows)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, volume)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed volumes: %w", err)
	}
	return volumes, nil
}

func (s *Store) ConfigureVolumePath(ctx context.Context, uuid, mountPath string, autoMount *bool) (Volume, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Volume{}, fmt.Errorf("begin volume path update: %w", err)
	}
	defer tx.Rollback()

	existing, err := scanVolume(tx.QueryRowContext(ctx, `
		SELECT uuid, mount_number, mount_path, auto_mount, device_serial, partition_number
		FROM volumes WHERE uuid = ?`, uuid))
	if err != nil {
		if err == sql.ErrNoRows {
			return Volume{}, apperr.New(http.StatusNotFound, apperr.CodeManagedVolumeNotFound, fmt.Sprintf("managed volume %s not found", uuid))
		}
		return Volume{}, fmt.Errorf("query managed volume: %w", err)
	}

	var usedByOther bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM volumes WHERE mount_path = ? AND uuid <> ?)`, mountPath, uuid).Scan(&usedByOther); err != nil {
		return Volume{}, fmt.Errorf("check volume mount path: %w", err)
	}
	if usedByOther {
		return Volume{}, apperr.New(http.StatusConflict, apperr.CodeMountPathInUse, fmt.Sprintf("mount path %s is already configured", mountPath))
	}
	nextAutoMount := existing.AutoMount
	if autoMount != nil {
		nextAutoMount = *autoMount
	}
	if _, err := tx.ExecContext(ctx, `UPDATE volumes SET mount_path = ?, auto_mount = ? WHERE uuid = ?`, mountPath, boolToInt(nextAutoMount), uuid); err != nil {
		return Volume{}, fmt.Errorf("update volume mount path: %w", err)
	}
	existing.MountPath = mountPath
	existing.AutoMount = nextAutoMount
	if err := tx.Commit(); err != nil {
		return Volume{}, fmt.Errorf("commit volume path update: %w", err)
	}
	return existing, nil
}

func (s *Store) SetVolumeAutoMount(ctx context.Context, uuid string, enabled bool) (Volume, error) {
	result, err := s.DB.ExecContext(ctx, `UPDATE volumes SET auto_mount = ? WHERE uuid = ?`, boolToInt(enabled), uuid)
	if err != nil {
		return Volume{}, fmt.Errorf("update volume auto-mount state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Volume{}, fmt.Errorf("count updated managed volumes: %w", err)
	}
	if affected != 1 {
		return Volume{}, apperr.New(http.StatusNotFound, apperr.CodeManagedVolumeNotFound, fmt.Sprintf("managed volume %s not found", uuid))
	}
	return s.VolumeByUUID(ctx, uuid)
}

func (s *Store) IsManagedUUID(ctx context.Context, uuid string) (bool, error) {
	if uuid == "" {
		return false, nil
	}
	var exists bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM volumes WHERE uuid = ?)`, uuid).Scan(&exists); err != nil {
		return false, fmt.Errorf("check managed volume UUID: %w", err)
	}
	return exists, nil
}

func (s *Store) AutoMountVolumes(ctx context.Context) ([]Volume, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT uuid, mount_number, mount_path, auto_mount, device_serial, partition_number
		FROM volumes WHERE auto_mount = 1 ORDER BY mount_number`)
	if err != nil {
		return nil, fmt.Errorf("list auto-mount volumes: %w", err)
	}
	defer rows.Close()

	volumes := make([]Volume, 0)
	for rows.Next() {
		volume, err := scanVolume(rows)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, volume)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auto-mount volumes: %w", err)
	}
	return volumes, nil
}

func (s *Store) DeleteVolumeIfExists(ctx context.Context, uuid string) error {
	if uuid == "" {
		return nil
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM volumes WHERE uuid = ?`, uuid); err != nil {
		return fmt.Errorf("delete managed volume: %w", err)
	}
	return nil
}

func (s *Store) DeleteVolumesBySerial(ctx context.Context, serial string) error {
	if serial == "" {
		return nil
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM volumes WHERE device_serial = ?`, serial); err != nil {
		return fmt.Errorf("delete managed volumes for disk: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanVolume(row rowScanner) (Volume, error) {
	var volume Volume
	var autoMount int
	if err := row.Scan(&volume.UUID, &volume.MountNumber, &volume.MountPath, &autoMount, &volume.DeviceSerial, &volume.PartitionNum); err != nil {
		return Volume{}, err
	}
	volume.AutoMount = autoMount == 1
	return volume, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
