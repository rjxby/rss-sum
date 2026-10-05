package server

import (
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5/middleware"
)

type JSON map[string]interface{}

var reMultWhtsp = regexp.MustCompile(`[\s\p{Zs}]{2,}`)

func Logger(l *log.Logger) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, 1)
			t1 := time.Now()
			defer func() {
				q := sanitizeLogValue(r.URL.RequestURI())
				l.Printf("[INFO] REST %s - %s - %s - %d (%d) - %v",
					r.Method, q, strings.Split(r.RemoteAddr, ":")[0],
					ww.Status(), ww.BytesWritten(), time.Since(t1))
			}()

			h.ServeHTTP(ww, r)
		})
	}
}

func sanitizeLogValue(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	return strings.TrimSpace(reMultWhtsp.ReplaceAllString(value, " "))
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		next.ServeHTTP(w, r)
	})
}
