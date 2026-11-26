//go:build windows

package editor

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	winCredTypeGeneric         = 1
	winCredPersistLocalMachine = 2
	winErrNotFound             = syscall.Errno(1168)
)

var (
	advapi32DLL   = syscall.NewLazyDLL("advapi32.dll")
	procCredWrite = advapi32DLL.NewProc("CredWriteW")
	procCredRead  = advapi32DLL.NewProc("CredReadW")
	procCredDel   = advapi32DLL.NewProc("CredDeleteW")
	procCredFree  = advapi32DLL.NewProc("CredFree")
)

type winCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type windowsKeychain struct{}

func platformKeychain() Keychain {
	return windowsKeychain{}
}

func winTargetName(service, account string) string {
	return service + "/" + account
}

// Set stores secret in the Windows Credential Manager using CredWriteW.
func (windowsKeychain) Set(service, account, secret string) error {
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	if service == "" || account == "" {
		return errors.New("editor: keychain service and account must be non-empty")
	}
	if secret == "" {
		return errors.New("editor: keychain secret must be non-empty")
	}
	targetPtr, err := syscall.UTF16PtrFromString(winTargetName(service, account))
	if err != nil {
		return err
	}
	userPtr, err := syscall.UTF16PtrFromString(account)
	if err != nil {
		return err
	}
	blob := []byte(secret)
	cred := winCredential{
		Type:               winCredTypeGeneric,
		TargetName:         targetPtr,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            winCredPersistLocalMachine,
		UserName:           userPtr,
	}
	r1, _, callErr := procCredWrite.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r1 == 0 {
		return fmt.Errorf("editor: windows credential manager write failed: %w", callErr)
	}
	return nil
}

// Get reads secret from the Windows Credential Manager using CredReadW.
func (windowsKeychain) Get(service, account string) (string, error) {
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	if service == "" || account == "" {
		return "", ErrCredentialNotFound
	}
	targetPtr, err := syscall.UTF16PtrFromString(winTargetName(service, account))
	if err != nil {
		return "", err
	}
	var credPtr *winCredential
	r1, _, callErr := procCredRead.Call(uintptr(unsafe.Pointer(targetPtr)), winCredTypeGeneric, 0, uintptr(unsafe.Pointer(&credPtr)))
	if r1 == 0 {
		if errors.Is(callErr, winErrNotFound) {
			return "", ErrCredentialNotFound
		}
		return "", fmt.Errorf("editor: windows credential manager read failed: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credPtr)))
	if credPtr == nil || credPtr.CredentialBlob == nil || credPtr.CredentialBlobSize == 0 {
		return "", ErrCredentialNotFound
	}
	data := unsafe.Slice(credPtr.CredentialBlob, credPtr.CredentialBlobSize)
	return string(data), nil
}

// Delete removes secret from the Windows Credential Manager using CredDeleteW.
func (windowsKeychain) Delete(service, account string) error {
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	if service == "" || account == "" {
		return nil
	}
	targetPtr, err := syscall.UTF16PtrFromString(winTargetName(service, account))
	if err != nil {
		return err
	}
	r1, _, callErr := procCredDel.Call(uintptr(unsafe.Pointer(targetPtr)), winCredTypeGeneric, 0)
	if r1 == 0 && !errors.Is(callErr, winErrNotFound) {
		return fmt.Errorf("editor: windows credential manager delete failed: %w", callErr)
	}
	return nil
}
