package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValueName = "fui"

func autostartCmd(args []string, port int) error {
	if len(args) != 1 {
		return errors.New("usage: fui autostart install|uninstall|status")
	}
	switch args[0] {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer key.Close()
		cmd := fmt.Sprintf(`"%s" serve --hidden --port %d`, exe, port)
		if err := key.SetStringValue(runValueName, cmd); err != nil {
			return err
		}
		fmt.Println("Autostart installed (HKCU Run key). The server will start at logon:")
		fmt.Println("  " + cmd)
		fmt.Println("Note: moving or deleting the executable breaks the entry; rerun 'autostart install' after moving it.")
		return nil
	case "uninstall":
		key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer key.Close()
		if err := key.DeleteValue(runValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		fmt.Println("Autostart removed.")
		return nil
	case "status":
		key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
		if err != nil {
			return err
		}
		defer key.Close()
		val, _, err := key.GetStringValue(runValueName)
		if errors.Is(err, registry.ErrNotExist) {
			fmt.Println("Autostart is NOT installed.")
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Println("Autostart is installed:")
		fmt.Println("  " + val)
		return nil
	default:
		return fmt.Errorf("unknown autostart subcommand %q", args[0])
	}
}
