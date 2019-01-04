package cmd

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	ui "github.com/gizak/termui"

	"github.com/briandowns/spinner"
	"github.com/golang/text/width"
	"github.com/spf13/cobra"
)

// testCmd represents the test command
var paperCmd = &cobra.Command{
	Use:   "paper",
	Short: "Random HeLab member for next week Journal Club",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:`,

	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Paper called")
		fmt.Println("============")
		lines, err := readLines("member.txt")
		if err != nil {
			log.Fatalf("readLines: %s", err)
		}
		randomMember(lines)
	},
}

func init() {
	rootCmd.AddCommand(paperCmd)
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func randomMember(s []string) {
	randomCount := make(map[string]int)
	for _, n := range s {
		fillSpace := strings.Repeat(" ", 10-getWidthUTF8String(n))
		fmt.Printf("|%s%s|\n", n, fillSpace)
		randomCount[n] = 0
	}
	for l := 1; l <= 10; l++ {
		for n, _ := range randomCount {
			randomCount[n] += rand.Intn(12)
			// progressBar := strings.Repeat("=", randomCount[n])
			// fmt.Printf("\033[2K\r%s  %s", n, progressBar)
			// time.Sleep(time.Second / 10)
		}
	}

	// TODO: dynamic update
	showUI(s)
	// runSpinner(2)
}

func runSpinner(ts int) {
	t := time.Duration(ts)
	spin := spinner.New(spinner.CharSets[35], 100*time.Millisecond) // Build our new spinner
	spin.Prefix = "Random Helab Member: "                           // Prefix text before the spinner
	spin.Suffix = "   ....."                                        // Append text after the spinner
	spin.Color("green")                                             // Set the spinner color to red
	spin.Start()                                                    // Start the spinner
	time.Sleep(t * time.Second)                                     // Run for some time to simulate work
	spin.Stop()
}

func getWidthUTF8String(s string) int {
	size := 0
	for _, runeValue := range s {
		p := width.LookupRune(runeValue)
		if p.Kind() == width.EastAsianWide {
			size += 2
			continue
		}
		if p.Kind() == width.EastAsianNarrow {
			size += 1
			continue
		}
		panic("cannot determine!")
	}
	return size
}

func getMaxValueOfMap(m map[string]int) int {
	maxNumber := 0
	for _, n := range m {
		if n > maxNumber {
			maxNumber = n
		}
	}
	return maxNumber
}

func showUI(s []string) {
	err := ui.Init()
	if err != nil {
		panic(err)
	}
	defer ui.Close()

	nameGauge := make(map[string]*ui.Gauge, len(s))
	nameCounts := make(map[string]int, len(s))
	randSteps := []int{0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 4}

	y := 1
	for _, n := range s {
		g := ui.NewGauge()
		g.Percent = 0
		g.Width = 100
		g.Height = 3
		g.Y = y
		g.BorderLabel = n
		g.BarColor = ui.ColorCyan
		g.BorderFg = ui.ColorWhite
		g.BorderLabelFg = ui.ColorCyan
		y += 3
		nameGauge[n] = g
	}

	updateG := func(count int) {
		if getMaxValueOfMap(nameCounts) < 100 {
			for n, g := range nameGauge {
				r := randSteps[rand.Intn(len(randSteps))]
				if nameCounts[n]+r > 100 {
					nameCounts[n] = 100
					g.Percent = 100
				} else {
					nameCounts[n] += r
					g.Percent += r
				}
				if g.Percent >= 100 {
					g.BarColor = ui.ColorRed
				}
				ui.Render(g)
			}
		} else {
			for _, g := range nameGauge {
				ui.Render(g)
			}
		}
		count++
	}

	tickerCount := 1
	uiEvents := ui.PollEvents()
	ticker := time.NewTicker(time.Second / 5).C
	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				return
			}
		case <-ticker:
			updateG(tickerCount)
			// tickerCount++
		}
	}

}
