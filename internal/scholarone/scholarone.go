// Package scholarone retrieves a user's paper and review status from ScholarOne
// Manuscripts journal sites (e.g. Information Systems Research, Management
// Science, MIS Quarterly).
//
// ScholarOne has no public API and its login + navigation are entirely
// JavaScript-driven (the Log In button and the Author/Review menu items submit a
// form carrying per-session tokens). Replaying that by hand over plain HTTP is
// brittle, so we drive a real headless Chrome the same way a person would: open
// the site, type the credentials, click Log In, then click into the Author and
// Reviewer dashboards and read the tables. The resulting HTML is parsed with
// goquery into the small structs below.
//
// Privacy: credentials arrive per request, are used only to fill the login form
// in memory, and are never written to disk or logged. Each retrieval runs in a
// throwaway browser profile that is discarded when the context is cancelled.
package scholarone

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

// A modern desktop Chrome UA; ScholarOne serves a different (lighter) page to
// unknown agents, so we look like a normal browser.
const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// SiteCreds is one journal site to retrieve, with the credentials to use for it.
type SiteCreds struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Paper is one row from the Author dashboard ("Manuscripts" queue).
type Paper struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Status           string   `json:"status"`           // decision / queue line(s)
	Editors          []string `json:"editors"`          // e.g. "SE: Zhang, Jingjing"
	SubmittingAuthor string   `json:"submittingAuthor"` // may be empty
	Created          string   `json:"created"`
	Submitted        string   `json:"submitted"`
}

// ReviewCell is a single labelled value in a review row. The reviewer dashboard
// columns vary slightly, so we keep them generic (label = column header).
type ReviewCell struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Review is one row from the Reviewer dashboard.
type Review struct {
	Columns []ReviewCell `json:"columns"`
}

// SiteResult is everything we retrieved for one site. A site-level Error means
// login/navigation failed; PaperError/ReviewError are per-section problems that
// still let the rest of the result through.
type SiteResult struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Papers      []Paper  `json:"papers"`
	Reviews     []Review `json:"reviews"`
	Error       string   `json:"error,omitempty"`
	PaperError  string   `json:"paperError,omitempty"`
	ReviewError string   `json:"reviewError,omitempty"`
}

var (
	submitRe = regexp.MustCompile(`(?i)Submitting Author:\s*(.+?)(?:\s+Cover Letter\b.*)?$`)
	wsRe     = regexp.MustCompile(`\s+`)
)

// Retrieve fetches all sites concurrently (each in its own browser) and returns
// the results in the same order. It never returns an error itself — every
// failure mode is reported inside the relevant SiteResult so the widget can show
// a per-journal fallback.
func Retrieve(ctx context.Context, sites []SiteCreds) []SiteResult {
	results := make([]SiteResult, len(sites))

	// Pre-flight: retrieval needs a Chrome/Chromium-family browser. If none is
	// installed, fail every site with one clear, actionable message rather than a
	// cryptic per-site driver error.
	if chromePath() == "" {
		const msg = "no supported browser found — retrieval runs a headless Chrome " +
			"in the background, so please install Google Chrome or Chromium. " +
			"(Firefox can't be used for retrieval.)"
		for i, c := range sites {
			results[i] = SiteResult{Key: c.Key, Name: c.Name, URL: c.URL, Error: msg}
		}
		return results
	}

	var wg sync.WaitGroup
	for i, c := range sites {
		wg.Add(1)
		go func(i int, c SiteCreds) {
			defer wg.Done()
			results[i] = retrieveSite(ctx, c)
		}(i, c)
	}
	wg.Wait()
	return results
}

// chromePath finds an installed Chrome/Chromium. Retrieval drives a headless
// Chrome (via the DevTools protocol), so a Chromium-family browser is required;
// Firefox cannot be driven this way. Empty means none was found anywhere we look.
func chromePath() string {
	// An explicit override wins (used by the Docker image) — but only if it
	// actually exists, so a stale CHROME_BIN can still fall back to discovery.
	if p := os.Getenv("CHROME_BIN"); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	// On PATH (Linux, and Windows when chrome.exe is on PATH).
	for _, name := range []string{"google-chrome", "google-chrome-stable",
		"chromium", "chromium-browser", "brave-browser", "microsoft-edge",
		"chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	// Well-known absolute locations not usually on PATH (macOS app bundles,
	// Windows installs).
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// retrieveSite logs into one site and scrapes its Author and Reviewer pages.
func retrieveSite(parent context.Context, c SiteCreds) SiteResult {
	res := SiteResult{Key: c.Key, Name: c.Name, URL: c.URL}
	if c.URL == "" || c.Username == "" || c.Password == "" {
		res.Error = "missing site URL, username, or password"
		return res
	}

	// A headless browser in a throwaway profile. no-sandbox keeps it working in
	// containers and across desktop setups without extra privileges.
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.WindowSize(1280, 900),
		chromedp.UserAgent(userAgent),
	)
	if p := chromePath(); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(parent, opts...)
	defer cancelAlloc()
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()
	// A hard ceiling per site so one stuck journal can't hang the whole request.
	ctx, cancelTimeout := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelTimeout()

	// Open the login page and submit the credentials.
	err := chromedp.Run(ctx,
		chromedp.Navigate(c.URL),
		chromedp.WaitVisible(`#USERID`, chromedp.ByQuery),
		chromedp.SendKeys(`#USERID`, c.Username, chromedp.ByQuery),
		chromedp.SendKeys(`#PASSWORD`, c.Password, chromedp.ByQuery),
		chromedp.Click(`#logInButton`, chromedp.ByQuery),
	)
	if err != nil {
		res.Error = "could not load the login page (" + cleanErr(err) + ")"
		return res
	}

	switch status, msg := waitLogin(ctx); status {
	case "ok":
		// logged in — fall through to scraping
	case "fail":
		if msg == "" {
			msg = "the User ID or Password was not accepted"
		}
		res.Error = "login failed: " + msg
		return res
	default: // "timeout"
		res.Error = "timed out after login — the site may need extra verification, " +
			"or the dashboard did not load"
		return res
	}

	res.Papers, res.PaperError = scrapeQueue(ctx, "AUTHOR_VIEW_MANUSCRIPTS",
		"authorDashboardQueue", "Author Center", parsePapers)
	res.Reviews, res.ReviewError = scrapeQueue(ctx, "REVIEWER_VIEW_MANUSCRIPTS",
		"reviewerDashboardQueue", "Review Center", parseReviews)
	return res
}

// waitLogin polls the page after the Log In click until it can tell whether we
// reached a dashboard ("ok"), the credentials were rejected ("fail" + message),
// or neither happened in time ("timeout"). It looks for the Author/Reviewer menu
// links (present once authenticated) versus the password field still showing
// with an error notice.
func waitLogin(ctx context.Context) (status, message string) {
	const js = `(function(){
		if (document.querySelector('a[href*="VIEW_MANUSCRIPTS"]')) return 'ok|';
		var bad = document.querySelector('#LOGIN_BAD_USERNAME_OR_PASSWORD');
		var badv = bad ? (bad.value || '') : '';
		var nd = document.querySelector('#notificationDiv');
		var msg = nd ? nd.innerText.replace(/\s+/g,' ').trim() : '';
		var hasPw = !!document.querySelector('#PASSWORD');
		if (hasPw && (badv || /not valid|incorrect|does not match|invalid|no match|try again|locked|required/i.test(msg)))
			return 'fail|' + (msg || badv);
		return 'wait|';
	})()`
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		var out string
		sub, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := chromedp.Run(sub, chromedp.Evaluate(js, &out))
		cancel()
		if err == nil {
			switch {
			case strings.HasPrefix(out, "ok|"):
				return "ok", ""
			case strings.HasPrefix(out, "fail|"):
				return "fail", clean(strings.TrimPrefix(out, "fail|"))
			}
		} else if ctx.Err() != nil {
			break // parent cancelled / overall timeout
		}
		time.Sleep(time.Second)
	}
	return "timeout", ""
}

// scrapeQueue clicks the menu link whose href targets nextPage (e.g.
// AUTHOR_VIEW_MANUSCRIPTS), waits for the queue table, grabs the page HTML, and
// hands it to parse. The generic-typed return keeps one function for both pages.
func scrapeQueue[T any](ctx context.Context, nextPage, queueID, center string,
	parse func(string) []T) ([]T, string) {

	linkSel := fmt.Sprintf(`a[href*="%s"]`, nextPage)

	// If the menu link is absent, this account simply doesn't hold that role.
	var hasLink bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		fmt.Sprintf(`!!document.querySelector('a[href*="%s"]')`, nextPage), &hasLink))
	if !hasLink {
		return nil, "This account has no " + center + "."
	}

	sub, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	var html string
	err := chromedp.Run(sub,
		chromedp.Click(linkSel, chromedp.ByQuery),
		waitForQueue(queueID),
		chromedp.OuterHTML(`html`, &html, chromedp.ByQuery),
	)
	if err != nil {
		return nil, "could not open the " + center + " (" + cleanErr(err) + ")"
	}
	return parse(html), ""
}

// waitForQueue waits until the queue table has rendered, or a "no submissions"
// marker has, or a short budget elapses. It always returns nil so the caller
// still captures the page (an empty queue is a valid, non-error outcome).
func waitForQueue(queueID string) chromedp.Action {
	js := fmt.Sprintf(`(function(){
		if (document.querySelector('#%s tbody tr')) return true;
		return !!document.querySelector('[data-label="NoResults"]');
	})()`, queueID)
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for i := 0; i < 45; i++ {
			var ready bool
			sub, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := chromedp.Run(sub, chromedp.Evaluate(js, &ready))
			cancel()
			if err == nil && ready {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(700 * time.Millisecond)
		}
		return nil // give up waiting; the parser handles an empty/absent table
	})
}

// parsePapers reads the Author dashboard queue into Paper rows.
func parsePapers(html string) []Paper {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var papers []Paper
	doc.Find("#authorDashboardQueue tbody tr").Each(func(_ int, tr *goquery.Selection) {
		idCell := tr.Find(`td[data-label="ID"]`)
		titleCell := tr.Find(`td[data-label="title"]`)
		statusCell := tr.Find(`td[data-label="status"]`)
		if idCell.Length() == 0 && titleCell.Length() == 0 {
			return // not a manuscript row (e.g. a stray/empty row)
		}
		p := Paper{
			ID:        firstText(idCell),
			Title:     firstText(titleCell),
			Created:   firstText(tr.Find(`td[data-label="created"]`)),
			Submitted: firstText(tr.Find(`td[data-label="submitted"]`)),
		}
		// Decision / queue lines (e.g. "Under Review", "Major Revision (…)").
		var lines []string
		statusCell.Find(".pagecontents").Each(func(_ int, s *goquery.Selection) {
			if t := clean(s.Text()); t != "" {
				lines = append(lines, t)
			}
		})
		p.Status = strings.Join(lines, " · ")
		// Handling editors (SE/EIC/ME/DE/ADM…).
		statusCell.Find("nobr").Each(func(_ int, s *goquery.Selection) {
			if t := clean(s.Text()); t != "" {
				p.Editors = append(p.Editors, t)
			}
		})
		if m := submitRe.FindStringSubmatch(clean(titleCell.Text())); len(m) == 2 {
			p.SubmittingAuthor = clean(m[1])
		}
		papers = append(papers, p)
	})
	return papers
}

// parseReviews reads the Reviewer dashboard queue. Columns differ per site, so
// each cell is paired with its header label and kept generic.
func parseReviews(html string) []Review {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	table := doc.Find("#reviewerDashboardQueue")
	if table.Length() == 0 {
		return nil
	}
	var headers []string
	table.Find("thead th").Each(func(_ int, s *goquery.Selection) {
		headers = append(headers, clean(s.Text()))
	})
	var reviews []Review
	table.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
		if tr.Find(`[data-label="NoResults"]`).Length() > 0 {
			return // the "There are no submissions in this queue" row
		}
		var cells []string
		tr.Find("td").Each(func(_ int, td *goquery.Selection) {
			cells = append(cells, clean(td.Text()))
		})
		nonEmpty := false
		for _, c := range cells {
			if c != "" {
				nonEmpty = true
				break
			}
		}
		if !nonEmpty {
			return
		}
		if len(cells) == 1 && strings.Contains(strings.ToLower(cells[0]), "no submission") {
			return
		}
		cols := make([]ReviewCell, 0, len(cells))
		for i, c := range cells {
			label := ""
			if i < len(headers) {
				label = headers[i]
			}
			cols = append(cols, ReviewCell{Label: label, Value: c})
		}
		reviews = append(reviews, Review{Columns: cols})
	})
	return reviews
}

// firstText returns the first non-empty direct text node of a selection — used
// to pull the leading value out of a cell that also holds links/sub-tables.
func firstText(s *goquery.Selection) string {
	if s.Length() == 0 {
		return ""
	}
	out := ""
	s.Contents().EachWithBreak(func(_ int, c *goquery.Selection) bool {
		if goquery.NodeName(c) == "#text" {
			if t := clean(c.Text()); t != "" {
				out = t
				return false
			}
		}
		return true
	})
	return out
}

func clean(s string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// cleanErr trims a driver error to something short and credential-free for the UI.
func cleanErr(err error) string {
	msg := clean(err.Error())
	if len(msg) > 140 {
		msg = msg[:140] + "…"
	}
	return msg
}
