package cli

import (
	"fmt"
	"os"

	"github.com/orchestra/orchestra/internal/instrument"
	"github.com/spf13/cobra"
)

var instrumentDryRun bool

var instrumentCmd = &cobra.Command{
	Use:   "instrument [dir]",
	Short: "Добавить OTel SDK инструментацию в проект",
	Long: `Определяет язык(и) проекта, устанавливает OTel SDK пакеты,
записывает файл инициализации трассировки и патчит точку входа.

По умолчанию работает в текущей директории. Идемпотентна: если
инструментация уже добавлена — пропускает без изменений.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}

		langs := instrument.Detect(dir, instrument.AllLangs)
		if len(langs) == 0 {
			fmt.Println("No supported languages detected.")
			return nil
		}

		detected := make([]string, len(langs))
		for i, l := range langs {
			detected[i] = l.Name
		}
		fmt.Printf("Detected: %v\n", detected)

		prefix := ""
		if instrumentDryRun {
			prefix = "[dry-run] "
		}

		results, err := instrument.Instrument(dir, langs, instrumentDryRun)
		for _, r := range results {
			if r.Skipped {
				fmt.Printf("%s: skipped — %s\n", r.Lang, r.SkipReason)
				continue
			}
			fmt.Printf("%s%s: wrote %s\n", prefix, r.Lang, r.TelemetryFile)
			if r.Patched {
				fmt.Printf("%s%s: patched entry point → %s\n", prefix, r.Lang, r.PatchedFile)
			}
			if r.InstallOutput != "" {
				fmt.Printf("%s%s: packages installed:\n%s\n", prefix, r.Lang, r.InstallOutput)
			}
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return err
		}

		if !instrumentDryRun {
			fmt.Println("\nDone. Start the OTel receiver with:")
			fmt.Println("  orchestra runtime serve")
		}
		return nil
	},
}

func init() {
	instrumentCmd.Flags().BoolVar(&instrumentDryRun, "dry-run", false, "показать что будет сделано без записи файлов")
	rootCmd.AddCommand(instrumentCmd)
}
