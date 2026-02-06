package ocr

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Preprocessor handles image preprocessing for optimal OCR/AI results
type Preprocessor struct {
	scaleForEasyOCR bool
}

// NewPreprocessor creates a new image preprocessor
func NewPreprocessor(scaleForEasyOCR bool) *Preprocessor {
	return &Preprocessor{
		scaleForEasyOCR: scaleForEasyOCR,
	}
}

// PreprocessImage reads and enhances an image file for better OCR/AI reading
func (p *Preprocessor) PreprocessImage(imagePath string) ([]byte, error) {
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, err
	}
	return p.PreprocessImageFromBytes(imageData)
}

// PreprocessImageFromBytes applies image enhancement filters
// Uses ImageMagick for: grayscale, contrast, denoise, sharpen
func (p *Preprocessor) PreprocessImageFromBytes(imageData []byte) ([]byte, error) {
	// Create temp files
	tmpDir := os.TempDir()
	inputFile := filepath.Join(tmpDir, fmt.Sprintf("input_%d.jpg", os.Getpid()))
	outputFile := filepath.Join(tmpDir, fmt.Sprintf("output_%d.jpg", os.Getpid()))

	// Write input image
	if err := os.WriteFile(inputFile, imageData, 0644); err != nil {
		return imageData, nil // Fallback to original
	}
	defer os.Remove(inputFile)
	defer os.Remove(outputFile)

	// Try ImageMagick processing
	// Pipeline: resize (if too large) -> grayscale -> contrast -> denoise -> sharpen
	args := []string{
		inputFile,
		// Resize if larger than 2000px (keeps aspect ratio)
		"-resize", "2000x2000>",
		// Convert to grayscale for better text contrast
		"-colorspace", "Gray",
		// Normalize histogram (auto-contrast)
		"-normalize",
		// Increase contrast
		"-contrast-stretch", "2%x1%",
		// Light denoise
		"-despeckle",
		// Sharpen text edges
		"-sharpen", "0x1",
		// Slight unsharp mask for text clarity
		"-unsharp", "0x0.5+0.5+0",
		// High quality output
		"-quality", "95",
		outputFile,
	}

	// Try 'magick' first (ImageMagick 7), fallback to 'convert' (ImageMagick 6)
	var cmd *exec.Cmd
	if _, err := exec.LookPath("magick"); err == nil {
		cmd = exec.Command("magick", args...)
	} else {
		cmd = exec.Command("convert", args...)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If ImageMagick fails, return original image
		fmt.Printf("[Preprocessor] ImageMagick failed: %v - %s\n", err, stderr.String())
		return imageData, nil
	}

	// Read processed image
	processed, err := os.ReadFile(outputFile)
	if err != nil {
		return imageData, nil // Fallback to original
	}

	fmt.Printf("[Preprocessor] Image enhanced: %d bytes -> %d bytes\n", len(imageData), len(processed))
	return processed, nil
}

// PreprocessForStamp applies special processing for images with stamps/seals
// More aggressive contrast to make stamp text readable
func (p *Preprocessor) PreprocessForStamp(imageData []byte) ([]byte, error) {
	tmpDir := os.TempDir()
	inputFile := filepath.Join(tmpDir, fmt.Sprintf("stamp_in_%d.jpg", os.Getpid()))
	outputFile := filepath.Join(tmpDir, fmt.Sprintf("stamp_out_%d.jpg", os.Getpid()))

	if err := os.WriteFile(inputFile, imageData, 0644); err != nil {
		return imageData, nil
	}
	defer os.Remove(inputFile)
	defer os.Remove(outputFile)

	// More aggressive processing for stamps
	args := []string{
		inputFile,
		"-resize", "2500x2500>",
		"-colorspace", "Gray",
		// Adaptive threshold for stamps (better for uneven lighting)
		"-lat", "50x50+10%",
		// Higher contrast
		"-contrast-stretch", "5%x2%",
		"-despeckle",
		"-despeckle",
		"-sharpen", "0x2",
		"-quality", "95",
		outputFile,
	}

	var cmd *exec.Cmd
	if _, err := exec.LookPath("magick"); err == nil {
		cmd = exec.Command("magick", args...)
	} else {
		cmd = exec.Command("convert", args...)
	}

	if err := cmd.Run(); err != nil {
		// Fallback to standard preprocessing
		return p.PreprocessImageFromBytes(imageData)
	}

	processed, err := os.ReadFile(outputFile)
	if err != nil {
		return p.PreprocessImageFromBytes(imageData)
	}

	fmt.Printf("[Preprocessor] Stamp-enhanced: %d bytes -> %d bytes\n", len(imageData), len(processed))
	return processed, nil
}

// SaveProcessedImage saves preprocessed image to file (for debugging)
func (p *Preprocessor) SaveProcessedImage(imageBytes []byte, outputPath string) error {
	return os.WriteFile(outputPath, imageBytes, 0644)
}
