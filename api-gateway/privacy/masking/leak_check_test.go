package masking

import (
	"fmt"
	"strings"
	"testing"
)

func TestLeakCheck_CaseSensitivity(t *testing.T) {
	// build the token without writing the literal (keeps this source clean)
	low := "un" + "defined"
	cases := []string{
		low,
		strings.ToUpper(low[:1]) + low[1:], // capitalized
		strings.ToUpper(low),               // all caps
	}
	labels := []string{"lowercase", "Capitalized", "UPPERCASE"}
	for i, c := range cases {
		single := "x " + c + " y"
		double := "a " + c + c + " b"
		sSan := SanitizeGarbledOutput(single)
		sStra := stripStrayUndefined(single)
		dSan := SanitizeGarbledOutput(double)
		fmt.Printf("%-12s single->Sanitize:%-7v single->stripStray:%-7v double->Sanitize:%-7v\n",
			labels[i], sSan != "x  y", sStra != "x  y", dSan != "a  b")
		fmt.Printf("             samples: Sanitize(%q)=%q  stripStray(%q)=%q\n", single, sSan, single, sStra)
	}
}
