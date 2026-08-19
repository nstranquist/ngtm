package gtmcli

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"github.com/nstranquist/ngtm/gtm"
)

func launchAssets(prog string, args []string, out, errOut io.Writer) int {
	product, rest := popProduct(args)
	fs, _, asJSON := launchFlagSet(prog+" launch assets", errOut)
	pack := fs.String("pack", "", "directory written by ndev browser shot|record --preset product-hunt")
	openComposer := fs.Bool("open", false, "open the Product Hunt composer in the default browser")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if product == "" {
		fmt.Fprintln(errOut, prog+" launch assets: product is required")
		return 2
	}
	if strings.TrimSpace(*pack) == "" {
		fmt.Fprintln(errOut, prog+" launch assets: --pack <dir> is required")
		return 2
	}
	kit, err := gtm.InspectProductHuntPack(product, *pack)
	if err != nil {
		fmt.Fprintln(errOut, prog+" launch assets:", err)
		return 1
	}
	if *asJSON {
		b, err := json.MarshalIndent(kit, "", "  ")
		if err != nil {
			fmt.Fprintln(errOut, prog+" launch assets:", err)
			return 1
		}
		_, _ = out.Write(append(b, '\n'))
	} else {
		fmt.Fprintf(out, "Product Hunt assets for %s\n", kit.Product)
		fmt.Fprintf(out, "composer  %s\n", kit.ComposerURL)
		fmt.Fprintf(out, "pack      %s\n", kit.PackDir)
		if kit.Title != "" {
			fmt.Fprintf(out, "title     %s\n", kit.Title)
		}
		for _, a := range kit.Assets {
			status := "ok"
			if !a.OK {
				status = "FAIL"
			}
			if a.Width > 0 {
				fmt.Fprintf(out, "  %-10s %s  %dx%d  %s\n", a.Slot, status, a.Width, a.Height, a.Path)
				continue
			}
			fmt.Fprintf(out, "  %-10s %s  %s\n", a.Slot, status, a.Path)
		}
		fmt.Fprintln(out, "API token can read the catalog and follow users; it cannot create a post.")
		fmt.Fprintln(out, "Upload these files in the composer. Record the live URL with launch posted.")
		for _, e := range kit.Errors {
			fmt.Fprintf(errOut, "  error: %s\n", e)
		}
	}
	if *openComposer {
		if err := openURL(kit.ComposerURL); err != nil {
			fmt.Fprintln(errOut, prog+" launch assets: open:", err)
			return 1
		}
	}
	if !kit.Ready {
		return 1
	}
	return 0
}

func openURL(raw string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", raw)
	case "linux":
		cmd = exec.Command("xdg-open", raw)
	default:
		return fmt.Errorf("open is not supported on %s", runtime.GOOS)
	}
	return cmd.Start()
}
