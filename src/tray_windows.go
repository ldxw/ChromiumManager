//go:build windows

package main

import (
	_ "embed"
	"log"
	"os/exec"
	"syscall"

	"fyne.io/systray"
)

//go:embed icon.ico
var trayIcon []byte

const uiURL = "http://" + serverAddr

// openUI hands the local URL to the default browser. rundll32 is used instead of
// `cmd /c start` so no console window flashes; HideWindow keeps it silent.
func openUI() {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", uiURL)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		log.Printf("[Tray] failed to open browser: %v", err)
	}
}

// runUI blocks on the Windows system tray until the user picks Quit.
func runUI() {
	systray.Run(onTrayReady, func() {})
}

func onTrayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("ChromiumManager")
	systray.SetTooltip("ChromiumManager - " + uiURL)
	systray.SetOnTapped(openUI)

	openItem := systray.AddMenuItem("打开界面", uiURL)
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出", "停止服务并退出")

	go func() {
		for {
			select {
			case <-openItem.ClickedCh:
				openUI()
			case <-quitItem.ClickedCh:
				// Shut the server down off the event loop: Quit must stay reachable.
				go func() {
					stopServer()
					systray.Quit()
				}()
				return
			}
		}
	}()
}
