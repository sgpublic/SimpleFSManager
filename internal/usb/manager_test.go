package usb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/sgpublic/simplefsmanager/internal/storage"
)

type fakeStorage struct {
	mu               sync.Mutex
	disks            []storage.Disk
	mounts           int
	mountErr         error
	firstListEntered chan<- struct{}
	releaseFirstList <-chan struct{}
	listCalls        int
}

func (s *fakeStorage) List(context.Context) ([]storage.Disk, error) {
	s.mu.Lock()
	s.listCalls++
	call := s.listCalls
	s.mu.Unlock()
	if call == 1 && s.firstListEntered != nil {
		s.firstListEntered <- struct{}{}
		<-s.releaseFirstList
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneDisks(s.disks), nil
}

func (s *fakeStorage) Partition(_ context.Context, path string) (storage.Disk, storage.Partition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, disk := range s.disks {
		for _, partition := range disk.Partitions {
			if partition.Path == path {
				return disk, partition, nil
			}
		}
	}
	return storage.Disk{}, storage.Partition{}, fmt.Errorf("partition %s not found", path)
}

func (s *fakeStorage) Mount(_ context.Context, path, target, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mountErr != nil {
		return s.mountErr
	}
	for diskIndex := range s.disks {
		for partitionIndex := range s.disks[diskIndex].Partitions {
			partition := &s.disks[diskIndex].Partitions[partitionIndex]
			if partition.Path == path {
				if len(partition.Mountpoints) > 0 {
					return fmt.Errorf("refusing to modify mounted partition %s", path)
				}
				partition.Mountpoints = []string{target}
				s.mounts++
				return nil
			}
		}
	}
	return fmt.Errorf("partition %s not found", path)
}

func (s *fakeStorage) Unmount(target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for diskIndex := range s.disks {
		for partitionIndex := range s.disks[diskIndex].Partitions {
			partition := &s.disks[diskIndex].Partitions[partitionIndex]
			if len(partition.Mountpoints) > 0 && partition.Mountpoints[0] == target {
				partition.Mountpoints = nil
				return nil
			}
		}
	}
	return fmt.Errorf("mount %s not found", target)
}

func cloneDisks(disks []storage.Disk) []storage.Disk {
	cloned := make([]storage.Disk, len(disks))
	for i, disk := range disks {
		cloned[i] = disk
		cloned[i].Partitions = append([]storage.Partition(nil), disk.Partitions...)
		for j := range cloned[i].Partitions {
			cloned[i].Partitions[j].Mountpoints = append([]string(nil), disk.Partitions[j].Mountpoints...)
		}
	}
	return cloned
}

func usbDisk(mountpoints []string) storage.Disk {
	return storage.Disk{
		Path: "/dev/sda",
		USB:  true,
		Partitions: []storage.Partition{{
			Path:        "/dev/sda1",
			Number:      1,
			FileSystem:  "ext4",
			Mountpoints: mountpoints,
		}},
	}
}

func TestReconcileSerializesSnapshotAndMount(t *testing.T) {
	firstListEntered := make(chan struct{})
	releaseFirstList := make(chan struct{})
	backend := &fakeStorage{
		disks:            []storage.Disk{usbDisk(nil)},
		firstListEntered: firstListEntered,
		releaseFirstList: releaseFirstList,
	}
	manager := New(backend)

	first := make(chan error, 1)
	go func() { first <- manager.Reconcile(context.Background(), "/dev/sda") }()
	<-firstListEntered
	second := make(chan error, 1)
	go func() { second <- manager.Reconcile(context.Background(), "/dev/sda") }()
	close(releaseFirstList)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.mounts != 1 {
		t.Fatalf("mounts = %d, want 1", backend.mounts)
	}
}

func TestReconcileReturnsMountFailure(t *testing.T) {
	mountErr := errors.New("input/output error")
	backend := &fakeStorage{disks: []storage.Disk{usbDisk(nil)}, mountErr: mountErr}
	err := New(backend).Reconcile(context.Background(), "/dev/sda")
	if !errors.Is(err, mountErr) {
		t.Fatalf("error = %v, want wrapped %v", err, mountErr)
	}
}

func TestReconcileKeepsManualUnmountSuppressed(t *testing.T) {
	backend := &fakeStorage{disks: []storage.Disk{usbDisk([]string{"/usbaa"})}}
	manager := New(backend)
	if err := manager.Reconcile(context.Background(), "/dev/sda"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Unmount(context.Background(), "/dev/sda1"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.mounts != 0 {
		t.Fatalf("mounts = %d, want 0", backend.mounts)
	}
}

func TestReconcileReusesLetterAfterRemoval(t *testing.T) {
	backend := &fakeStorage{disks: []storage.Disk{usbDisk(nil)}}
	manager := New(backend)
	manager.detachDisk = func(rune) {}
	if err := manager.Reconcile(context.Background(), "/dev/sda"); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	backend.disks = nil
	backend.mu.Unlock()
	if err := manager.Reconcile(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	backend.disks = []storage.Disk{usbDisk(nil)}
	backend.mu.Unlock()
	if err := manager.Reconcile(context.Background(), "/dev/sda"); err != nil {
		t.Fatal(err)
	}
	if letter := manager.letters["/dev/sda"]; letter != 'a' {
		t.Fatalf("letter = %q, want 'a'", letter)
	}
}
