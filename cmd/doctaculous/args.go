package main

// reorderArgs moves non-flag arguments after flags so positional inputs may
// appear before flags (flag.Parse stops at the first non-flag token). valueFlags
// names the flags that take their value as a separate token ("--flag value");
// the "--flag=value" form is always safe.
func reorderArgs(args []string, valueFlags map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ { //nolint:intrange // index i is mutated inside the loop
		a := args[i]
		if len(a) > 0 && a[0] == '-' {
			flags = append(flags, a)
			if valueFlags[a] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...)
}
