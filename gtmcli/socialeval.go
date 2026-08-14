package gtmcli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nstranquist/ngtm/gtm"
)

func cmdSocialEval(prog string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(prog+" social eval", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fixturePath := fs.String("fixture", "", "versioned social eval fixture (default: embedded golden v1)")
	asJSON := fs.Bool("json", false, "emit JSON")
	strict := fs.Bool("strict", false, "exit 3 when the fixture is unstable or misses a threshold")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		_, _ = fmt.Fprintln(errOut, prog+" social eval: unexpected positional arguments")
		return 2
	}
	raw := gtm.DefaultSocialEvalFixture()
	if path := strings.TrimSpace(*fixturePath); path != "" {
		var err error
		raw, err = os.ReadFile(path)
		if err != nil {
			_, _ = fmt.Fprintln(errOut, prog+" social eval:", err)
			return 1
		}
	}
	report, err := gtm.EvaluateSocialFixture(raw)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, prog+" social eval:", err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintln(errOut, prog+" social eval:", err)
			return 1
		}
	} else {
		_, _ = fmt.Fprint(out, report.Markdown())
	}
	if *strict && !report.Passed {
		return 3
	}
	return 0
}
