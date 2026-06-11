package tui

import (
	"os/exec"
	"runtime"

	"github.com/roeyazroel/linear-tui/internal/logger"
)

// openURL opens a URL in the user's platform default browser.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		logger.Warning("tui.platform: unsupported OS for opening URLs os=%s", runtime.GOOS)
		return nil
	}
	if err := cmd.Start(); err != nil {
		logger.ErrorWithErr(err, "tui.platform: failed to open URL url=%s", url)
		return err
	}
	logger.Debug("tui.platform: opened URL in browser url=%s", url)
	return nil
}

// copyToClipboard copies text to the system clipboard.
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	case "windows":
		cmd = exec.Command("clip")
	default:
		logger.Warning("tui.platform: unsupported OS for clipboard operations os=%s", runtime.GOOS)
		return nil
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		logger.ErrorWithErr(err, "tui.platform: failed to get stdin pipe for clipboard command")
		return err
	}
	if err := cmd.Start(); err != nil {
		logger.ErrorWithErr(err, "tui.platform: failed to start clipboard command")
		return err
	}
	if _, err := stdin.Write([]byte(text)); err != nil {
		logger.ErrorWithErr(err, "tui.platform: failed to write to clipboard")
		return err
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		logger.ErrorWithErr(err, "tui.platform: clipboard command failed")
		return err
	}
	logger.Debug("tui.platform: copied to clipboard text_length=%d", len(text))
	return nil
}
