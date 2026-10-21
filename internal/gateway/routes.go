package gateway

import (
	"fmt"
	"regexp"
	"strings"
)

type route struct {
	config      RouteConfig
	re          *regexp.Regexp
	methods     map[string]bool
	specificity int
}

func compileRoute(c RouteConfig) (route, error) {
	if c.Name == "" {
		c.Name = c.Pattern
	}
	if c.Pattern == "" || c.Upstream == "" {
		return route{}, fmt.Errorf("route %q needs pattern and upstream", c.Name)
	}
	p := c.Pattern
	if strings.HasPrefix(p, "re:") {
		p = strings.TrimPrefix(p, "re:")
	} else {
		p = compilePathPattern(p)
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return route{}, fmt.Errorf("route %q: invalid pattern: %w", c.Name, err)
	}
	m := make(map[string]bool, len(c.Methods))
	for _, method := range c.Methods {
		m[strings.ToUpper(method)] = true
	}
	return route{config: c, re: re, methods: m, specificity: routeSpecificity(c.Pattern)}, nil
}

// compilePathPattern recognizes a named segment, a one-segment wildcard and a
// recursive wildcard. It deliberately escapes every other regex metacharacter.
func compilePathPattern(pattern string) string {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i += 2
			} else {
				b.WriteString("[^/]*")
				i++
			}
		case '{':
			end := strings.IndexByte(pattern[i:], '}')
			if end > 1 {
				name := pattern[i+1 : i+end]
				if validParameterName(name) {
					b.WriteString("[^/]+")
					i += end + 1
					continue
				}
			}
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		}
	}
	b.WriteByte('$')
	return b.String()
}

func validParameterName(name string) bool {
	if name == "" || !((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) {
		return false
	}
	for _, c := range name[1:] {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func routeSpecificity(pattern string) int {
	if strings.HasPrefix(pattern, "re:") {
		return 0
	}
	return len(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(pattern, "**", ""), "*", ""), "{", ""))
}
func (r route) matches(path, method string) bool {
	return r.re.MatchString(path) && (len(r.methods) == 0 || r.methods[method])
}
