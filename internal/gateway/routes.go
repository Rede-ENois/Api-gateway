package gateway

import (
	"fmt"
	"regexp"
	"strings"
)

// matcher is the compiled path pattern shared by proxy routes and rate limit rules.
type matcher struct {
	re          *regexp.Regexp
	methods     map[string]bool
	specificity int
}

type route struct {
	matcher
	config RouteConfig
}

func compileRoute(c RouteConfig) (route, error) {
	if c.Name == "" {
		c.Name = c.Pattern
	}
	if c.Pattern == "" || c.Upstream == "" {
		return route{}, fmt.Errorf("route %q needs pattern and upstream", c.Name)
	}
	m, err := compileMatcher(c.Pattern, c.Methods)
	if err != nil {
		return route{}, fmt.Errorf("route %q: %w", c.Name, err)
	}
	return route{matcher: m, config: c}, nil
}

func compileMatcher(pattern string, methods []string) (matcher, error) {
	p := pattern
	if strings.HasPrefix(p, "re:") {
		p = strings.TrimPrefix(p, "re:")
	} else {
		p = compilePathPattern(p)
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return matcher{}, fmt.Errorf("invalid pattern: %w", err)
	}
	m := make(map[string]bool, len(methods))
	for _, method := range methods {
		m[strings.ToUpper(method)] = true
	}
	return matcher{re: re, methods: m, specificity: routeSpecificity(pattern)}, nil
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
				if parameterName.MatchString(name) {
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

var parameterName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// routeSpecificity scores each segment so literals beat parameters, parameters
// beat one-segment wildcards and those beat recursive wildcards. Regex routes
// score 0. On a tie the route declared first wins.
func routeSpecificity(pattern string) int {
	if strings.HasPrefix(pattern, "re:") {
		return 0
	}
	score := 0
	for _, segment := range strings.Split(strings.Trim(pattern, "/"), "/") {
		switch {
		case strings.Contains(segment, "**"):
		case strings.Contains(segment, "*"):
			score++
		case strings.Contains(segment, "{"):
			score += 2
		default:
			score += 3
		}
	}
	return score
}
func (r matcher) matches(path, method string) bool {
	return r.re.MatchString(path) && (len(r.methods) == 0 || r.methods[method])
}
