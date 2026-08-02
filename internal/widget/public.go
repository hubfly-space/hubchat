package widget

import (
	"context"
	"math/rand/v2"
	"net/url"
	"strings"
)

// PublicConfig is the narrowed projection a visitor's browser is allowed to
// see — no inbox id, no workspace id, no domain list, no rollout percentage,
// no internal counters (migration 0007, web/widget/src/types.ts's own
// doc comment: "a visitor holding a public key should learn nothing about
// the workspace beyond what they can already see on screen").
type PublicConfig struct {
	WidgetID   string
	Language   string
	Enabled    bool
	Online     bool
	Modes      []string
	Appearance map[string]any
	Content    map[string]any
	Behavior   map[string]any
	// Articles is a small published-only prefetch for instant widget search;
	// deeper search and article bodies use the origin-gated widget endpoints.
	Articles []map[string]any
}

// ResolveConfig answers the loader's boot() request: is this key valid, is
// the calling page's origin allowed, and if so, what should render.
//
// pageURL is the page the visitor is actually on (v1.js sends it as a query
// parameter, not relying solely on the Origin header, because a same-site
// navigation can arrive without one). originHeader is a defense-in-depth
// fallback when pageURL is missing or unparsable.
func (s *Service) ResolveConfig(ctx context.Context, publicKey, pageURL, originHeader string) (*PublicConfig, error) {
	return s.ResolveConfigForLanguage(ctx, publicKey, pageURL, originHeader, "")
}

// ResolveConfigForLanguage projects the configured copy into the visitor's
// requested locale. The public response contains only the selected strings,
// never the complete translation catalog.
func (s *Service) ResolveConfigForLanguage(ctx context.Context, publicKey, pageURL, originHeader, language string) (*PublicConfig, error) {
	widget, err := s.WidgetForOrigin(ctx, publicKey, pageURL, originHeader)
	if err != nil {
		return nil, err
	}

	if err := s.repo.touchInstall(ctx, widget.ID); err != nil {
		return nil, err
	}

	online, err := s.repo.anyMemberOnline(ctx, widget.WorkspaceID)
	if err != nil {
		return nil, err
	}

	// Rollout gates the whole widget rather than individual behaviour: a
	// visitor either gets the configured experience or, for the excluded
	// share, none at all (the loader already treats enabled:false as "do not
	// load the interface").
	included := widget.RolloutPercent >= 100 || rand.IntN(100) < widget.RolloutPercent

	return &PublicConfig{
		WidgetID:   widget.ID,
		Language:   normalizeLanguage(language),
		Enabled:    widget.Enabled && included,
		Online:     online,
		Modes:      widget.Modes,
		Appearance: widget.Appearance,
		Content:    localizedContent(widget.Content, language),
		Behavior:   widget.Behavior,
		Articles:   []map[string]any{},
	}, nil
}

func normalizeLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "_", "-")))
	if len(value) > 32 {
		return ""
	}
	return value
}

func localizedContent(content map[string]any, language string) map[string]any {
	result := make(map[string]any, len(content))
	for key, value := range content {
		if key != "translations" {
			result[key] = value
		}
	}

	translations, ok := content["translations"].(map[string]any)
	if !ok {
		return result
	}
	normalized := normalizeLanguage(language)
	for _, key := range []string{normalized, strings.Split(normalized, "-")[0]} {
		if key == "" {
			continue
		}
		variant, ok := translations[key].(map[string]any)
		if !ok {
			continue
		}
		for field, value := range variant {
			result[field] = value
		}
		break
	}
	return result
}

// WidgetForOrigin is the shared gate every unauthenticated widget endpoint
// opens with: does this public key exist, and is the calling page's origin
// on that widget's allowlist. Every visitor-facing handler — config,
// starting a conversation, replying, identify, track — calls this before
// doing anything else, so a stolen public key handed to a different origin
// gets nothing anywhere in the surface, not just at the config request.
func (s *Service) WidgetForOrigin(ctx context.Context, publicKey, pageURL, originHeader string) (*Widget, error) {
	widget, err := s.repo.byPublicKey(ctx, publicKey)
	if err != nil {
		return nil, err
	}

	hostname := hostnameOf(pageURL)
	if hostname == "" {
		hostname = hostnameOf(originHeader)
	}
	if hostname == "" {
		return nil, ErrOriginNotAllowed
	}

	domains, err := s.repo.domains(ctx, widget.ID)
	if err != nil {
		return nil, err
	}
	if !originAllowed(domains, hostname) {
		return nil, ErrOriginNotAllowed
	}
	return widget, nil
}

// hostnameOf extracts a hostname from either a full page URL or a bare
// "scheme://host" origin string, tolerating whichever this caller has.
func hostnameOf(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// originAllowed checks hostname against the allowlist: an exact match, or a
// "*.example.com" entry matching any subdomain (never the apex itself —
// that needs its own entry, so an owner allowlisting subdomains does not
// silently also allowlist a same-named apex they never reviewed).
func originAllowed(domains []Domain, hostname string) bool {
	for _, d := range domains {
		if d.Domain == hostname {
			return true
		}
		if suffix, ok := strings.CutPrefix(d.Domain, "*."); ok {
			if strings.HasSuffix(hostname, "."+suffix) {
				return true
			}
		}
	}
	return false
}
