package file

import (
	"fmt"
	"strings"
	"time"
)

type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) ProcessMessage(message, filename string) (string, error) {
	if filename == "" {
		return "", fmt.Errorf("filename cannot be empty")
	}

	filename = m.normalizeFilename(filename)
	content := m.formatContent(message, filename)

	return content, nil
}

func (m *Manager) normalizeFilename(filename string) string {
	filename = strings.ToLower(filename)
	if !strings.HasSuffix(filename, ".md") {
		filename += ".md"
	}
	return filename
}

func (m *Manager) formatContent(message, filename string) string {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	
	if strings.ToLower(filename) == "todo.md" {
		return fmt.Sprintf("- [ ] %s\n", message)
	}
	
	return fmt.Sprintf("%s - %s\n", timestamp, message)
}

func (m *Manager) ParseMessage(text string) (filename, content string, err error) {
	parts := strings.SplitN(text, " ", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("message must contain filename and content separated by space")
	}

	filename = m.normalizeFilename(parts[0])
	
	formattedContent := m.formatContent(parts[1], filename)
	
	return filename, formattedContent, nil
}