package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	texFile    = "traffic_simulation.tex"
	outputName = "traffic_simulation"
)

func main() {
	fmt.Println("=== LaTeX to PDF Renderer ===")
	fmt.Println()

	// Проверяем наличие .tex файла
	if _, err := os.Stat(texFile); os.IsNotExist(err) {
		log.Fatalf("Ошибка: файл %s не найден", texFile)
	}

	// Проверяем наличие pdflatex
	if err := checkCommand("pdflatex"); err != nil {
		log.Fatal("Ошибка: pdflatex не установлен. Установите TeX Live или MiKTeX")
	}

	fmt.Printf("Компиляция %s...\n", texFile)
	fmt.Println()

	// Компилируем LaTeX файл дважды (для корректных ссылок)
	for i := 1; i <= 2; i++ {
		fmt.Printf("Проход %d/2...\n", i)
		if err := runPdflatex(texFile); err != nil {
			log.Fatalf("Ошибка при компиляции (проход %d): %v", i, err)
		}
	}

	// Очищаем временные файлы
	fmt.Println()
	fmt.Println("Очистка временных файлов...")
	cleanupTempFiles(outputName)

	pdfFile := outputName + ".pdf"
	if _, err := os.Stat(pdfFile); err == nil {
		fmt.Println()
		fmt.Printf("✓ Успешно! PDF создан: %s\n", pdfFile)

		// Получаем абсолютный путь
		absPath, _ := filepath.Abs(pdfFile)
		fmt.Printf("  Полный путь: %s\n", absPath)

		// Получаем размер файла
		info, _ := os.Stat(pdfFile)
		fmt.Printf("  Размер: %.2f KB\n", float64(info.Size())/1024)
	} else {
		log.Fatal("Ошибка: PDF файл не был создан")
	}
}

// checkCommand проверяет наличие команды в системе
func checkCommand(command string) error {
	_, err := exec.LookPath(command)
	return err
}

// runPdflatex запускает pdflatex для компиляции .tex файла
func runPdflatex(texFile string) error {
	cmd := exec.Command("pdflatex", "-interaction=nonstopmode", texFile)

	// Захватываем вывод
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Выводим последние строки лога для диагностики
		lines := strings.Split(string(output), "\n")
		fmt.Println("Последние строки вывода:")
		start := len(lines) - 20
		if start < 0 {
			start = 0
		}
		for _, line := range lines[start:] {
			if strings.TrimSpace(line) != "" {
				fmt.Println("  ", line)
			}
		}
		return err
	}

	// Выводим краткую информацию об успехе
	outputStr := string(output)
	if strings.Contains(outputStr, "Output written on") {
		// Извлекаем информацию о страницах
		for _, line := range strings.Split(outputStr, "\n") {
			if strings.Contains(line, "Output written on") {
				fmt.Println("  ", strings.TrimSpace(line))
				break
			}
		}
	}

	return nil
}

// cleanupTempFiles удаляет временные файлы LaTeX
func cleanupTempFiles(basename string) {
	extensions := []string{".aux", ".log", ".out", ".toc"}

	for _, ext := range extensions {
		filename := basename + ext
		if err := os.Remove(filename); err == nil {
			fmt.Printf("  Удален: %s\n", filename)
		}
	}
}
