package importxuicmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/importxui"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func Run(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("import-xui", flag.ContinueOnError)
	fs.SetOutput(out)
	var src string
	var dryRun bool
	var strategy string
	var reportPath string
	var yes bool
	var includeHistory bool
	var includeRouting bool
	var host string
	fs.StringVar(&src, "src", "", "path to x-ui.db")
	fs.BoolVar(&dryRun, "dry-run", false, "preview import without committing changes")
	fs.StringVar(&strategy, "strategy", string(importxui.StrategyMerge), "conflict strategy: merge, replace or skip")
	fs.StringVar(&reportPath, "report", "", "write JSON report to path")
	fs.BoolVar(&yes, "yes", false, "confirm non-dry-run import")
	fs.BoolVar(&includeHistory, "include-history", false, "import aggregated historical traffic")
	fs.BoolVar(&includeRouting, "include-routing", false, "import Xray routing rules best-effort")
	fs.StringVar(&host, "host", "", "hostname baked into imported clients' subscription links (defaults to the configured sub/web domain)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	src = strings.TrimSpace(src)
	if src == "" {
		fmt.Fprintln(out, "import-xui: --src is required")
		return 2
	}
	importStrategy := importxui.Strategy(strategy)
	if err := importStrategy.Validate(); err != nil {
		fmt.Fprintln(out, "import-xui:", err)
		return 2
	}
	if !dryRun && !yes {
		fmt.Fprint(out, "This will import into the active solovey-ui dbsqlite. Type 'yes' to continue: ")
		var answer string
		if _, err := fmt.Fscan(os.Stdin, &answer); err != nil || answer != "yes" {
			fmt.Fprintln(out, "import-xui: cancelled")
			return 1
		}
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		fmt.Fprintln(out, "import-xui:", err)
		return 1
	}
	if !dryRun {
		backupPath, err := importxui.WritePreImportBackup(time.Now().Unix())
		if err != nil {
			fmt.Fprintln(out, "import-xui:", err)
			return 1
		}
		fmt.Fprintln(out, "Pre-import backup:", backupPath)
	}
	plan, err := importxui.Plan(src, importxui.PlanOptions{
		Strategy:       importStrategy,
		AdminMode:      importxui.AdminModeSkip,
		IncludeHistory: includeHistory,
		IncludeRouting: includeRouting,
	})
	if err != nil {
		fmt.Fprintln(out, "import-xui:", err)
		return 1
	}
	report, err := importxui.Apply(src, *plan, importxui.ApplyOptions{
		DryRun:     dryRun,
		SkipBackup: true,
		Hostname:   strings.TrimSpace(host),
	})
	if err != nil {
		fmt.Fprintln(out, "import-xui:", err)
		return 1
	}
	printImportXuiSummary(out, report)
	if reportPath != "" {
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(out, "import-xui:", err)
			return 1
		}
		if err := os.WriteFile(reportPath, raw, 0o600); err != nil {
			fmt.Fprintln(out, "import-xui:", err)
			return 1
		}
	}
	return 0
}

func printImportXuiSummary(out io.Writer, report *importxui.Report) {
	fmt.Fprintf(out, "Import summary:\n")
	fmt.Fprintf(out, "  Inbounds: %d/%d imported, %d skipped, %d conflicts\n",
		report.Summary.Inbounds.Imported,
		report.Summary.Inbounds.Total,
		report.Summary.Inbounds.Skipped,
		report.Summary.Inbounds.Conflicts,
	)
	fmt.Fprintf(out, "  Endpoints: %d imported\n", report.Summary.Endpoints.Imported)
	fmt.Fprintf(out, "  TLS: %d created, %d reused\n", report.Summary.TLS.Created, report.Summary.TLS.Reused)
	fmt.Fprintf(out, "  Clients: %d unique, %d created, %d merged\n",
		report.Summary.Clients.UniqueEmails,
		report.Summary.Clients.Created,
		report.Summary.Clients.Merged,
	)
	if len(report.Warnings) > 0 {
		fmt.Fprintf(out, "  Warnings: %d\n", len(report.Warnings))
	}
}
