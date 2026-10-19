package main

import "golang.org/x/sys/windows"

// hideConsole detaches the process from its console window so the autostart
// entry doesn't leave a terminal open at logon.
func hideConsole() {
	windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole").Call()
}
