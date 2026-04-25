package cmd

import "fmt"

func installDaemon(_ string) error {
	return fmt.Errorf("daemon install not yet supported on Windows — use WSL or run prysm mesh join directly")
}

func uninstallDaemon() error {
	return fmt.Errorf("daemon uninstall not yet supported on Windows")
}
