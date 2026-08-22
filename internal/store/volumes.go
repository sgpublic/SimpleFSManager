package store

import (
	"context"
	"database/sql"
	"fmt"
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
// It does not mount the filesystem.
func (s *Store) ConfigureVolume(ctx context.Context, uuid, serial string, partitionNumber int, mountPath string) (Volume, error) {
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
			return Volume{}, fmt.Errorf("mount path %s is already configured", mountPath)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE volumes SET mount_path = ?, auto_mount = 1 WHERE uuid = ?`, mountPath, uuid); err != nil {
			return Volume{}, fmt.Errorf("update volume mount path: %w", err)
		}
		volume.MountPath = mountPath
		volume.AutoMount = true
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
		return Volume{}, fmt.Errorf("mount path %s is already configured", mountPath)
	}
	volume = Volume{
		UUID:         uuid,
		MountNumber:  mountNumber,
		MountPath:    mountPath,
		AutoMount:    true,
		DeviceSerial: serial,
		PartitionNum: partitionNumber,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO volumes (uuid, device_serial, partition_number, mount_number, mount_path, auto_mount)
		VALUES (?, ?, ?, ?, ?, 1)`, volume.UUID, volume.DeviceSerial, volume.PartitionNum, volume.MountNumber, volume.MountPath); err != nil {
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
			return Volume{}, fmt.Errorf("managed volume %s not found", uuid)
		}
		return Volume{}, fmt.Errorf("query managed volume: %w", err)
	}
	return volume, nil
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

func (s *Store) DeleteVolume(ctx context.Context, uuid string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM volumes WHERE uuid = ?`, uuid)
	if err != nil {
		return fmt.Errorf("delete managed volume: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return fmt.Errorf("count deleted managed volumes: %w", err)
		}
		return fmt.Errorf("managed volume %s not found", uuid)
	}
	return nil
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
