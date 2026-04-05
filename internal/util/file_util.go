package util

import (
	"bytes"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ExtractTextFromPDF extracts plain text from a PDF file's raw bytes.
func ExtractTextFromPDF(data []byte) (string, error) {
	r := bytes.NewReader(data)
	pdfReader, err := pdf.NewReader(r, int64(len(data)))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	for i := 1; i <= pdfReader.NumPage(); i++ {
		p := pdfReader.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
	}
	return buf.String(), nil
}

// IsImage returns true for common image file extensions.
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
	case ".txt", ".csv", ".json", ".md", ".go", ".py", ".js", ".ts", ".svg", ".xml", ".html", ".css", ".yml", ".yaml", ".sh":
		return true
	}
	return false
}
