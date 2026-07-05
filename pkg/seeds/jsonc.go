package seeds

// stripJSONComments removes // line comments and /* block comments */ from
// JSONC input, and strips trailing commas before } or ], producing valid
// standard JSON. String literals are preserved verbatim.
func stripJSONComments(data string) string {
	src := []byte(data)
	out := make([]byte, 0, len(src))
	i := 0

	for i < len(src) {
		c := src[i]

		// String literal — copy verbatim including escape sequences
		if c == '"' {
			out = append(out, c)
			i++
			for i < len(src) {
				out = append(out, src[i])
				if src[i] == '\\' && i+1 < len(src) {
					i++
					out = append(out, src[i])
				} else if src[i] == '"' {
					break
				}
				i++
			}
			i++
			continue
		}

		// Line comment
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}

		// Block comment
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) {
				if src[i] == '*' && src[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}

		// Trailing comma — look ahead past whitespace for } or ]
		if c == ',' {
			j := i + 1
			for j < len(src) && isJSONWhitespace(src[j]) {
				j++
			}
			// Also skip over any comments between the comma and closing bracket
			for j+1 < len(src) {
				if src[j] == '/' && src[j+1] == '/' {
					j += 2
					for j < len(src) && src[j] != '\n' {
						j++
					}
					if j < len(src) {
						j++ // skip the newline
					}
					for j < len(src) && isJSONWhitespace(src[j]) {
						j++
					}
				} else if src[j] == '/' && src[j+1] == '*' {
					j += 2
					for j+1 < len(src) {
						if src[j] == '*' && src[j+1] == '/' {
							j += 2
							break
						}
						j++
					}
					for j < len(src) && isJSONWhitespace(src[j]) {
						j++
					}
				} else {
					break
				}
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				// Skip the trailing comma
				i++
				continue
			}
		}

		out = append(out, c)
		i++
	}

	return string(out)
}

func isJSONWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
