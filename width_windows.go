package main

// terminalWidth is 0 on Windows, so setup never redraws a row there.
func terminalWidth() int {
	return 0
}
