package util

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ExtractTextFromPDF extracts plain text from a PDF file's raw bytes. The PDF
// library panics on some malformed files, so that is turned into an error.
func ExtractTextFromPDF(data []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("malformed PDF: %v", r)
		}
	}()
	pdfReader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	for i := 1; i <= pdfReader.NumPage(); i++ {
		p := pdfReader.Page(i)
		if p.V.IsNull() {
			continue
		}
		pageText, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(pageText)
		buf.WriteString("\n")
	}
	return buf.String(), nil
}

// IsImage returns true for image types the vision models accept. SVG is
// deliberately text: it is markup, not pixels.
func IsImage(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	}
	return false
}

// IsText returns true for common text-based file extensions.
func IsText(ext string) bool {
	switch strings.ToLower(ext) {
	case ".txt", ".csv", ".json", ".md", ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".c", ".h",
		".cpp", ".rs", ".rb", ".php", ".sql", ".svg", ".xml", ".html", ".css", ".yml", ".yaml", ".toml",
		".ini", ".sh", ".log":
		return true
	}
	return false
}

// IsSupportedUpload reports whether the app can do anything with the file.
func IsSupportedUpload(ext string) bool {
	return IsImage(ext) || IsText(ext) || strings.ToLower(ext) == ".pdf"
}
