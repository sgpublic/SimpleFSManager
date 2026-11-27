package apperr

import (
	"errors"
	"net/http"
)

const (
	CodeAuthAlreadyConfigured   = "auth_already_configured"
	CodeAuthLocalUserRequired   = "auth_local_user_required"
	CodeAuthSystemAuthFailed    = "auth_system_auth_failed"
	CodeAuthPasswordTooShort    = "auth_password_too_short"
	CodeAuthSetupRequired       = "auth_setup_required"
	CodeAuthInvalidCredentials  = "auth_invalid_credentials"
	CodeAuthRequired            = "auth_required"
	CodeAuthRateLimited         = "auth_rate_limited"
	CodeConfirmationFailed      = "confirmation_failed"
	CodeSystemDiskProtected     = "system_disk_protected"
	CodeMountedDiskProtected    = "mounted_disk_protected"
	CodeInvalidDisk             = "invalid_disk"
	CodeInvalidPartition        = "invalid_partition"
	CodeInvalidPartitionTable   = "invalid_partition_table"
	CodePartitionNotFound       = "partition_not_found"
	CodePartitionPresent        = "partition_present"
	CodeUnsupportedFilesystem   = "unsupported_filesystem"
	CodeInvalidZonedPartSize    = "invalid_zoned_partition_size"
	CodeUnformattedPartition    = "unformatted_partition"
	CodeInsufficientSpace       = "insufficient_space"
	CodeInvalidMountTarget      = "invalid_mount_target"
	CodeInvalidMountPath        = "invalid_mount_path"
	CodeMountPathInUse          = "mount_path_in_use"
	CodeMountPathNotConfigured  = "mount_path_not_configured"
	CodeManagedVolumeNotFound   = "managed_volume_not_found"
	CodeUSBMutationNotSupported = "usb_mutation_not_supported"
	CodeNotUSBPartition         = "not_usb_partition"
	CodeUSBCapacityExceeded     = "usb_capacity_exceeded"
	CodeReclaimNotAvailable     = "reclaim_not_available"
	CodeReclaimToolFailed       = "reclaim_tool_failed"
	CodeSmartQueryFailed        = "smart_query_failed"
	CodeRebootFailed            = "reboot_failed"
	CodeInternalError           = "internal_error"
)

// Error represents a structured domain error with a stable API error code and
// HTTP status code.
type Error struct {
	Code    string
	Status  int
	Message string
	Params  map[string]any
	Err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	return e.Err
}

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func Wrap(status int, code, message string, err error) *Error {
	return &Error{Status: status, Code: code, Message: message, Err: err}
}

func WithParams(status int, code, message string, params map[string]any) *Error {
	return &Error{Status: status, Code: code, Message: message, Params: params}
}

func Code(err error) string {
	var appErr *Error
	if errors.As(err, &appErr) && appErr.Code != "" {
		return appErr.Code
	}
	return CodeInternalError
}

func Status(err error) int {
	var appErr *Error
	if errors.As(err, &appErr) && appErr.Status != 0 {
		return appErr.Status
	}
	return http.StatusInternalServerError
}

func Params(err error) map[string]any {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Params
	}
	return nil
}

func IsCode(err error, code string) bool {
	return Code(err) == code
}
