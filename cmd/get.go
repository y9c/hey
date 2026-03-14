package cmd

import (
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Get information from SAM or other biological files",
	Long:  `Subcommands to extract specific information from genomic data files.`,
}

func init() {
	rootCmd.AddCommand(getCmd)
}
