// Package term provides interactive terminal input functions.
// It replaces promptui to avoid the readline package's Windows console init(),
// which would otherwise affect all commands including --help.
package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// isInteractive returns true if stdin is a terminal.
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// Select shows a numbered list and returns the selected index.
func Select(label string, items []string) (int, string, error) {
	if !isInteractive() {
		return 0, "", fmt.Errorf("not a terminal")
	}

	fmt.Println(label + ":")
	for i, item := range items {
		fmt.Printf("  %d. %s\n", i+1, item)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("Enter choice (1-%d): ", len(items))
		input, err := reader.ReadString('\n')
		if err != nil {
			return 0, "", err
		}
		input = strings.TrimSpace(input)

		var n int
		if _, err := fmt.Sscanf(input, "%d", &n); err != nil || n < 1 || n > len(items) {
			fmt.Printf("Invalid input. Enter a number between 1 and %d.\n", len(items))
			continue
		}

		return n - 1, items[n-1], nil
	}
}

// Prompt asks for text input. If mask is 0, input is shown; otherwise masked.
func Prompt(label string, mask rune, defaultValue string, allowEdit bool) (string, error) {
	if !isInteractive() {
		return "", fmt.Errorf("not a terminal")
	}

	promptText := label
	if defaultValue != "" {
		promptText += " [" + defaultValue + "]"
	}
	promptText += ": "

	var input string
	var err error

	if mask != 0 {
		// Password-style input with masking
		fmt.Fprint(os.Stderr, promptText)
		var byteInput []byte
		byteInput, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		input = string(byteInput)
	} else {
		// Plain text input
		fmt.Print(promptText)
		reader := bufio.NewReader(os.Stdin)
		input, err = reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		input = strings.TrimRight(input, "\r\n")
	}

	if input == "" && defaultValue != "" {
		return defaultValue, nil
	}

	return input, nil
}

// Password prompts for a password with masking.
func Password(label string) (string, error) {
	return Prompt(label, '*', "", false)
}

// PasswordWithConfirm prompts for a password twice and returns it if they match.
func PasswordWithConfirm(label string) (string, error) {
	pwd, err := Password(label)
	if err != nil {
		return "", err
	}

	confirm, err := Password("Confirm " + label)
	if err != nil {
		return "", err
	}

	if pwd != confirm {
		return "", fmt.Errorf("passwords do not match")
	}

	return pwd, nil
}

// YesNo asks a yes/no question and returns true if the answer is y/Y.
// defaultAnswer sets the default when Enter is pressed without input.
func YesNo(label, defaultAnswer string) bool {
	if !isInteractive() {
		return defaultAnswer == "y" || defaultAnswer == "Y"
	}

	promptText := label
	if defaultAnswer != "" {
		promptText += " (" + defaultAnswer + "/N)"
	}
	promptText += ": "

	fmt.Fprint(os.Stderr, promptText)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return defaultAnswer == "y" || defaultAnswer == "Y"
	}
	return input == "y" || input == "yes"
}
