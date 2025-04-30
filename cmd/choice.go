package cmd

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	// Imports for termui
	ui "github.com/gizak/termui/v3"
	widgets "github.com/gizak/termui/v3/widgets"

	// Import for table display
	table "github.com/aquasecurity/table"
	// Import for colored output
	color "github.com/fatih/color"

	// Import for width calculation
	"github.com/golang/text/width"
	"github.com/spf13/cobra"
)

// Variable to store the path provided by the -i flag
var inputMemberFile string

// choiceCmd represents the choice command.
var choiceCmd = &cobra.Command{
	Use:   "choice [-i FILE | ITEM1 ITEM2 ...]", // Usage string showing both input methods
	Short: "Helps you make a random selection from a list",
	Long: `Performs a random selection from a list of items provided either directly
as command-line arguments or from a file.

How to use:
  1. Provide items as arguments:
     hey choice itemA itemB "Item C with spaces" itemD

  2. Provide items via a file (one item per line):
     hey choice -i path/to/your/items.txt

Details:
  - If both arguments and the -i flag are given, arguments take precedence.
  - The command visualizes the random selection process using an animation which
    stops once the first item reaches 100%.
  - After the animation (quit with 'q' or Ctrl+C), it prints the final selected item
    and displays the full list again in a table.
  - Empty lines in the input file are ignored.

(Note: This command was originally created for selecting HeLab members for Journal Club.)`, // Retained note about original purpose
	RunE: func(cmd *cobra.Command, args []string) error { // Using RunE for better error handling
		var lines []string
		var err error
		source := "" // Keep track of where items came from

		// 1. Prioritize command-line arguments
		if len(args) > 0 {
			lines = args
			source = "command-line arguments"
		} else if inputMemberFile != "" { // 2. Check if the file flag was used
			lines, err = readLines(inputMemberFile)
			if err != nil {
				return fmt.Errorf("error reading file '%s': %w", inputMemberFile, err)
			}
			if len(lines) > 0 {
				source = fmt.Sprintf("file '%s'", inputMemberFile)
			} else {
				source = fmt.Sprintf("empty or invalid file '%s'", inputMemberFile)
			}
		} else {
			// 3. Neither arguments nor -i flag provided
			_ = cmd.Help() // Show help message
			return fmt.Errorf("no items provided. Use command-line arguments or the -i flag")
		}

		// Check if the list is empty after processing input
		if len(lines) == 0 {
			return fmt.Errorf("no items to choose from (list from %s is empty or invalid)", source)
		}

		fmt.Printf("Choosing from %d items provided via %s...\n", len(lines), source)
		fmt.Println("Starting visualization... Press 'q' or Ctrl+C to quit UI and see result.")

		// Perform the selection and display using the updated randomMember function
		randomMember(lines)
		return nil // Indicate success
	},
}

func init() {
	rootCmd.AddCommand(choiceCmd)
	choiceCmd.Flags().StringVarP(&inputMemberFile, "input", "i", "", "Input file containing a list of items (one per line)")
}

// readLines reads a file specified by path and returns a slice of non-empty strings,
// one for each line. Returns an error if the file cannot be opened or read.
func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return lines, fmt.Errorf("error scanning file: %w", err)
	}
	return lines, nil
}

// randomMember runs the termui visualization, then performs the actual random selection,
// prints the result, and finally shows the full list using a table.
func randomMember(items []string) {
	// Run the termui visualization first. It exits when user presses 'q' or Ctrl+C.
	showUI(items) // This function now handles its own UI setup/teardown

	// --- Code below executes *after* showUI() returns ---

	fmt.Println("\n--- Selection Result ---") // Add separator after UI closes

	// Perform the definitive random selection
	rand.Seed(time.Now().UnixNano()) // Re-seed just in case
	selectedIndex := rand.Intn(len(items))
	selectedItem := items[selectedIndex]

	// Print the selected item
	fmt.Print("Randomly selected: ")
	color.New(color.FgGreen, color.Bold).Printf("%s\n\n", selectedItem)

	// Display the full list of items using aquasecurity/table
	fmt.Println("Full List of Items:")
	t := table.New(os.Stdout)
	t.SetHeaders("Item")
	t.SetHeaderStyle(table.StyleBold)
	t.SetLineStyle(table.StyleBlue)
	t.SetDividers(table.UnicodeRoundedDividers)

	for _, item := range items {
		t.AddRow(item)
	}
	t.Render()
	fmt.Println() // Add a final newline
}

// getWidthUTF8String calculates the display width of a string, accounting for CJK characters.
func getWidthUTF8String(s string) int {
	size := 0
	props := width.Properties{}
	for _, runeValue := range s {
		props = width.LookupRune(runeValue)
		switch props.Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			size += 2
		case width.EastAsianAmbiguous:
			size += 1
		default:
			size += 1
		}
	}
	return size
}

// getMaxValueOfMap finds the maximum integer value in a map[string]int.
func getMaxValueOfMap(m map[string]int) int {
	maxNumber := 0
	if len(m) == 0 {
		return 0
	}
	first := true
	for _, n := range m {
		if first {
			maxNumber = n
			first = false
		} else if n > maxNumber {
			maxNumber = n
		}
	}
	return maxNumber
}

// showUI initializes and runs the termui-based visualization for random selection.
func showUI(items []string) {
	if err := ui.Init(); err != nil {
		fmt.Printf("\nWarning: Could not initialize UI for visualization (%v).\n", err)
		return
	}
	defer ui.Close()

	termWidth, termHeight := ui.TerminalDimensions()
	if termWidth <= 0 || termHeight <= 0 {
		fmt.Println("\nError: Invalid terminal size reported. Cannot display UI visualization.")
		return
	}

	nameGauge := make(map[string]*widgets.Gauge, len(items))
	nameCounts := make(map[string]int, len(items))
	randSteps := []int{0, 0, 1, 1, 1, 2, 2, 3, 4, 5}

	gaugeHeight := 3
	totalHeightNeeded := len(items) * gaugeHeight
	if totalHeightNeeded > termHeight {
		fmt.Printf("Warning: Terminal height (%d) might be too small for %d items. UI may overlap or be cut off.\n", termHeight, len(items))
	}

	yPos := 0
	maxUsableWidth := termWidth - 2
	if maxUsableWidth < 1 {
		maxUsableWidth = 1
	}

	for _, name := range items {
		if yPos+gaugeHeight > termHeight {
			break
		}
		g := widgets.NewGauge()
		g.Percent = 0
		g.Title = name
		g.TitleStyle.Fg = ui.ColorWhite
		g.TitleStyle.Modifier = ui.ModifierBold
		g.BarColor = ui.ColorBlue
		g.BorderStyle.Fg = ui.ColorWhite
		g.LabelStyle.Fg = ui.ColorYellow
		g.SetRect(0, yPos, maxUsableWidth, yPos+gaugeHeight)
		yPos += gaugeHeight
		nameGauge[name] = g
	}

	initialRenderables := make([]ui.Drawable, 0, len(nameGauge))
	for _, g := range nameGauge {
		initialRenderables = append(initialRenderables, g)
	}
	if len(initialRenderables) > 0 {
		ui.Render(initialRenderables...)
	}

	// --- Animation Loop ---
	updateGauges := func(currentTermWidth int) bool {
		// Check if a winner already exists *before* this update cycle
		maxVal := getMaxValueOfMap(nameCounts)
		winnerExists := (maxVal >= 100)

		renderables := make([]ui.Drawable, 0, len(nameGauge))
		newGaugeWidth := currentTermWidth - 2
		if newGaugeWidth < 1 {
			newGaugeWidth = 1
		}

		for name, g := range nameGauge {
			// *** Only update the percentage if no winner has been declared yet ***
			if !winnerExists {
				step := randSteps[rand.Intn(len(randSteps))]
				newPercent := nameCounts[name] + step
				if newPercent >= 100 {
					newPercent = 100
					g.BarColor = ui.ColorRed // Set winner color *only* when first hitting 100
					// Don't set winnerExists = true here, let the check at the start handle it next tick
				}
				nameCounts[name] = newPercent
				g.Percent = newPercent
			} // End of if !winnerExists

			// Resize logic always applies
			currentWidth := g.Dx()
			if currentWidth != newGaugeWidth {
				g.SetRect(0, g.Min.Y, newGaugeWidth, g.Min.Y+g.Dy())
			}

			renderables = append(renderables, g)
		}

		if len(renderables) > 0 {
			ui.Render(renderables...)
		}
		// Return true if a winner existed at the start of *this* tick
		return winnerExists
	}

	// Event handling loop
	uiEvents := ui.PollEvents()
	ticker := time.NewTicker(time.Millisecond * 150).C
	animationFinished := false // Flag to track if animation stopped

	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				return // Exit showUI immediately
			case "<Resize>":
				payload := e.Payload.(ui.Resize)
				termWidth = payload.Width
				termWidth, termHeight = ui.TerminalDimensions() // Update both
				ui.Clear()
				// Update gauges immediately with new width, respect animationFinished flag
				if !animationFinished {
					animationFinished = updateGauges(termWidth)
				} else {
					// If animation is finished, just redraw without updating percentages
					renderables := make([]ui.Drawable, 0, len(nameGauge))
					newGaugeWidth := termWidth - 2
					if newGaugeWidth < 1 {
						newGaugeWidth = 1
					}
					for _, g := range nameGauge {
						currentWidth := g.Dx()
						if currentWidth != newGaugeWidth {
							g.SetRect(0, g.Min.Y, newGaugeWidth, g.Min.Y+g.Dy())
						}
						renderables = append(renderables, g)
					}
					if len(renderables) > 0 {
						ui.Render(renderables...)
					}
				}
			}
		case <-ticker:
			// Only update if animation hasn't finished
			if !animationFinished {
				currentWidth, _ := ui.TerminalDimensions()
				if currentWidth != termWidth {
					termWidth = currentWidth
				}
				// updateGauges now returns true if a winner existed at the start of the tick
				animationFinished = updateGauges(termWidth)
			}
			// If animationFinished is true, the ticker effectively does nothing more
			// for the gauge percentages, but the loop continues listening for 'q' or resize.
		}
	}
} // End of showUI
