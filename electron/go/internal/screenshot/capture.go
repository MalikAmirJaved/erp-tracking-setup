package screenshot

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/kbinani/screenshot"
)

func CaptureScreen() error {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return fmt.Errorf("no display found")
	}

	// Fixed folder for screenshots
	dir := "D:\\trackingScreenShot"
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("cannot create folder: %v", err)
	}

	for i := 0; i < n; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			return err
		}

		fileName := fmt.Sprintf("screenshot_%d_%d.png", i, time.Now().Unix())
		filePath := filepath.Join(dir, fileName)

		file, err := os.Create(filePath)
		if err != nil {
			return err
		}
		defer file.Close()

		if err := png.Encode(file, img); err != nil {
			return err
		}
	}

	return nil
}
