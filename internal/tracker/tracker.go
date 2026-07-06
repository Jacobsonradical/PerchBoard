// Package tracker retrieves a user's paper and review status from manuscript
// systems. Each site names the system it runs on; supported so far:
//
//   - "scholarone" (default) — ScholarOne Manuscripts journal sites, e.g.
//     Information Systems Research, Management Science, MIS Quarterly
//     (this file);
//   - "pcs" — Precision Conference Solutions, used by conferences such as
//     ICIS and the SIGCHI family (pcs.go);
//   - "paperfox" — PaperFox.ai, a modern conference system, e.g. CIST
//     (paperfox.go).
//
// None of these systems has a public API, and ScholarOne's login + navigation
// are entirely JavaScript-driven (form submits carrying per-session tokens),
// so we drive a real headless Chrome the same way a person would: open the
// site, type the credentials, click log in, then click into the dashboards and
// read the tables. The resulting HTML is parsed with goquery into the small
// structs below, which are shared by every system driver.
//
// Privacy: credentials arrive per request, are used only to fill the login form
// in memory, and are never written to disk or logged. Each retrieval runs in a
// throwaway browser profile that is discarded when the context is cancelled.
package tracker

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

// SiteCreds is one site to retrieve, with the credentials to use for it.
// System selects the driver ("scholarone" when empty, or "pcs").
type SiteCreds struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	System   string `json:"system,omitempty"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Paper is one submission row (ScholarOne's Author dashboard, or PCS's
// Submissions page — the PCS-only fields are empty on ScholarOne rows and
// vice versa).
type Paper struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Status           string   `json:"status"`                     // decision / queue line(s)
	Editors          []string `json:"editors,omitempty"`          // e.g. "SE: Zhang, Jingjing"
	SubmittingAuthor string   `json:"submittingAuthor,omitempty"` // may be empty
	Created          string   `json:"created,omitempty"`
	Submitted        string   `json:"submitted,omitempty"`
	// PCS submissions table extras.
	Section  string   `json:"section,omitempty"`  // page section, e.g. "Past Submissions"
	Deadline string   `json:"deadline,omitempty"` // submission deadline
	Category string   `json:"category,omitempty"`
	Note     string   `json:"note,omitempty"`
	Actions  []string `json:"actions,omitempty"` // links offered in the row
}

// ReviewCell is a single labelled value in a review row — the fallback shape
// used when a site's reviewer queue doesn't carry the usual data-label cells.
type ReviewCell struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Review is one row from the Reviewer dashboard ("Manuscripts Awaiting Review"
// queue). The standard ScholarOne template labels the cells (action / dueDate /
// type / idTitle / status), which parses into the structured fields below; the
// action dropdown additionally carries the paper's abstract and the actions
// still open to the reviewer (e.g. "Continue Review"). Sites without those
// labels fall back to the generic Columns list.
type Review struct {
	Queue     string       `json:"queue,omitempty"` // sidebar list, e.g. "Review and Score", "Scores Submitted", "Invitations"
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	Type      string       `json:"type,omitempty"`      // e.g. "Research Article"
	DueDate   string       `json:"dueDate,omitempty"`   // Review and Score queue, e.g. "14-Jul-2026"
	Completed string       `json:"completed,omitempty"` // Scores Submitted queue
	Sent      string       `json:"sent,omitempty"`      // Invitations queue (invite sent date)
	Status    string       `json:"status,omitempty"`    // e.g. "Under Review"
	Editors   []string     `json:"editors,omitempty"`   // "SE: …", "EIC: …", …
	Actions   []string     `json:"actions,omitempty"`   // e.g. "Continue Review"
	Abstract  string       `json:"abstract,omitempty"`
	// Columns is the fallback for a row without the usual data-labels; on a
	// structured row it instead carries any data-labelled cells we don't
	// recognize, so an unexpected column still reaches the widget.
	Columns []ReviewCell `json:"columns,omitempty"`
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
	submitRe  = regexp.MustCompile(`(?i)Submitting Author:\s*(.+?)(?:\s+Cover Letter\b.*)?$`)
	wsRe      = regexp.MustCompile(`\s+`)
	wordRe    = regexp.MustCompile(`[A-Za-z0-9]`)                      // tells a real label from a "———" separator
	dateRe    = regexp.MustCompile(`[A-Z][a-z]+ \d{1,2}, \d{4}`)       // "June 5, 2026" (PaperFox)
	reviewsRe = regexp.MustCompile(`\d+\s*/\s*\d+\s+reviews?`)         // "0/2 reviews" (PaperFox)
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
			switch c.System {
			case "pcs":
				results[i] = retrievePCS(ctx, c)
			case "paperfox":
				results[i] = retrievePaperFox(ctx, c)
			default: // "" or "scholarone"
				results[i] = retrieveScholarOne(ctx, c)
			}
		}(i, c)
	}
	wg.Wait()
	return results
}

// newSiteBrowser starts a fresh headless Chrome in a throwaway profile for one
// site's retrieval, with a hard per-site time ceiling so one stuck site can't
// hang the whole request. Call the returned cleanup when done.
func newSiteBrowser(parent context.Context, ceiling time.Duration) (context.Context, func()) {
	// no-sandbox keeps it working in containers and across desktop setups
	// without extra privileges.
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
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	ctx, cancelTimeout := context.WithTimeout(ctx, ceiling)
	return ctx, func() { cancelTimeout(); cancelCtx(); cancelAlloc() }
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

// retrieveScholarOne logs into one ScholarOne site and scrapes its Author and
// Reviewer pages.
func retrieveScholarOne(parent context.Context, c SiteCreds) SiteResult {
	res := SiteResult{Key: c.Key, Name: c.Name, URL: c.URL}
	if c.URL == "" || c.Username == "" || c.Password == "" {
		res.Error = "missing site URL, username, or password"
		return res
	}

	// 3 min ceiling, not 2: walking the extra reviewer queues — Scores
	// Submitted, Invitations, possibly paginated — adds a few navigations.
	ctx, cleanup := newSiteBrowser(parent, 3*time.Minute)
	defer cleanup()

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
	// The Reviewer Center splits reviews across sidebar queues; the click above
	// only lands on the default one ("Review and Score"). Pull the others too.
	if res.ReviewError == "" {
		res.Reviews, res.ReviewError = scrapeAllReviewQueues(ctx, res.Reviews)
	}
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
		// Stamp the outgoing page so the wait below can tell the next page from
		// this one. Without it, markers that exist on BOTH dashboards (e.g. an
		// empty author queue's NoResults cell while we head to the reviewer
		// page) would satisfy the wait early and we'd scrape the stale page.
		chromedp.Evaluate(`document.body && document.body.setAttribute('data-s1-stale','1')`, nil),
		chromedp.Click(linkSel, chromedp.ByQuery),
		waitForQueue(queueID),
		chromedp.OuterHTML(`html`, &html, chromedp.ByQuery),
	)
	if err != nil {
		return nil, "could not open the " + center + " (" + cleanErr(err) + ")"
	}
	return parse(html), ""
}

// waitForQueue waits (on a fresh page — see the data-s1-stale stamp) until the
// queue table has rendered, a "no submissions" marker has, or the dashboard
// shell (#navigationDIV) is up — the last one covers dashboards that render NO
// queue table at all, e.g. an Author Center with zero submissions lands on a
// "Start New Submission" view. Gives up quietly after a short budget so the
// caller still captures the page (an empty queue is a valid outcome).
func waitForQueue(queueID string) chromedp.Action {
	js := fmt.Sprintf(`(function(){
		if (document.body && document.body.hasAttribute('data-s1-stale')) return false;
		if (document.querySelector('#%s tbody tr')) return true;
		if (document.querySelector('[data-label="NoResults"]')) return true;
		return !!document.querySelector('#navigationDIV');
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

// reviewQueue is one entry in the Reviewer Center sidebar ("Scores Submitted",
// "Invitations", "Review and Score - Fast Track", …) with the list id carried
// by its javascript: link and the item count shown next to it ("36", "0", or
// blank — Invitations carries no count).
type reviewQueue struct {
	Name  string `json:"name"`
	List  string `json:"list"`
	Count string `json:"count"`
}

// discoverQueuesJS lists the non-active Reviewer Center sidebar queues:
// name (anchor text), list id (from the javascript: href), and the count shown
// in the item's leading <span> ("36", "0", or "" — Invitations has no count).
const discoverQueuesJS = `(function(){
	var out = [];
	document.querySelectorAll('#navigationDIV li').forEach(function(li){
		var a = li.querySelector('a[href*="MS_LIST_TO_DISPLAY_FOR_REVIEWER"]');
		if (!a) return;
		var m = (a.getAttribute('href') || '').match(/MS_LIST_TO_DISPLAY_FOR_REVIEWER'\s*,\s*(\d+)/);
		if (!m) return;
		var s = li.querySelector('span');
		out.push({
			name: a.textContent.replace(/\s+/g,' ').trim(),
			list: m[1],
			count: s ? s.textContent.trim() : '',
		});
	});
	return out;
})()`

// scrapeAllReviewQueues completes the review scrape. The rows already in hand
// come from the default sidebar queue (usually "Review and Score"); tag them
// with that queue's name, then visit every other sidebar queue — their links
// re-render the same #reviewerDashboardQueue table in place — and append those
// rows. Extra queues are best-effort: a failure reports which queue broke but
// keeps everything collected so far.
func scrapeAllReviewQueues(ctx context.Context, got []Review) ([]Review, string) {
	// Name the default view from the sidebar's active item.
	var active string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var a = document.querySelector('#navigationDIV li.active a');
		return a ? a.textContent.replace(/\s+/g,' ').trim() : '';
	})()`, &active))
	if active == "" {
		active = "In progress"
	}
	for i := range got {
		got[i].Queue = active
	}

	// Discover the other queues from the sidebar links (the active queue's own
	// entry has href="#", so it is naturally excluded). Each sidebar item also
	// shows its count in a leading <span>; a literal "0" means the queue is
	// empty and navigating there would be a wasted page load.
	var queues []reviewQueue
	_ = chromedp.Run(ctx, chromedp.Evaluate(discoverQueuesJS, &queues))

	for _, q := range queues {
		if q.Count == "0" {
			continue // sidebar says it's empty — skip the navigation
		}
		rows, errNote := scrapeReviewQueuePages(ctx, q)
		for i := range rows {
			rows[i].Queue = q.Name
		}
		got = append(got, rows...)
		if errNote != "" {
			return got, "could not fully read the \"" + q.Name + "\" queue (" + errNote + ")"
		}
	}
	return got, ""
}

// scrapeReviewQueuePages opens one sidebar queue and pages through it. Each
// page load asks for 25 rows (the largest option in the site's own
// items-per-page menu — an unlisted value might be rejected); the loop keeps
// fetching pages until one brings nothing new or comes back shorter than the
// 10-row minimum page size, meaning there is no next page.
func scrapeReviewQueuePages(ctx context.Context, q reviewQueue) ([]Review, string) {
	var all []Review
	seen := map[string]bool{}
	for page := 0; page < 10; page++ {
		rows, errNote := loadReviewQueuePage(ctx, q.List, page)
		if errNote != "" {
			return all, errNote
		}
		fresh := 0
		for _, r := range rows {
			if r.ID != "" && seen[r.ID] {
				continue // overlap with a previous page
			}
			if r.ID != "" {
				seen[r.ID] = true
			}
			all = append(all, r)
			fresh++
		}
		if fresh == 0 || len(rows) < 10 {
			break
		}
	}
	return all, ""
}

// loadReviewQueuePage navigates to one page of one reviewer queue and parses
// it. The sidebar links drive the site's own setField/setDataAndNextPage form
// machinery; we mark the current queue table stale before triggering it so the
// wait can tell the re-rendered table from the one being replaced.
func loadReviewQueuePage(ctx context.Context, list string, page int) ([]Review, string) {
	nav := fmt.Sprintf(`(function(){
		if (typeof setField !== 'function' || typeof setDataAndNextPage !== 'function') return false;
		if (document.body) document.body.setAttribute('data-s1-stale', '1');
		setField('DATATABLE_CURRENT_PAGE_REVIEWER_CENTER', %d);
		setField('DATATABLE_PAGE_LENGTH_REVIEWER_CENTER', 25);
		setDataAndNextPage('MS_LIST_TO_DISPLAY_FOR_REVIEWER', %s, 'REVIEWER_VIEW_MANUSCRIPTS');
		return true;
	})()`, page, list)

	sub, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var okNav bool
	if err := chromedp.Run(sub, chromedp.Evaluate(nav, &okNav)); err != nil {
		return nil, cleanErr(err)
	}
	if !okNav {
		return nil, "queue navigation is not available on this page"
	}
	var html string
	if err := chromedp.Run(sub,
		waitForFreshQueue(),
		chromedp.OuterHTML(`html`, &html, chromedp.ByQuery),
	); err != nil {
		return nil, cleanErr(err)
	}
	return parseReviews(html), ""
}

// waitForFreshQueue waits for the queue table to be re-rendered after a
// sidebar-queue navigation. The queue views re-render the SAME table id in
// place, so the outgoing page's body was stamped data-s1-stale just before
// navigating; "fresh" means an un-stamped page whose table has rendered (an
// empty queue still renders the table, with its NoResults row).
func waitForFreshQueue() chromedp.Action {
	const js = `(function(){
		if (document.body && document.body.hasAttribute('data-s1-stale')) return false;
		var t = document.querySelector('#reviewerDashboardQueue');
		return !!(t && t.querySelector('tbody tr'));
	})()`
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for i := 0; i < 40; i++ {
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
		return fmt.Errorf("the queue did not load in time")
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

// parseReviews reads the Reviewer dashboard queue. The standard ScholarOne
// template labels every cell with data-label (action/dueDate/type/idTitle/
// status), which parses into the structured Review fields; a row without those
// labels is kept generically as header→value columns so an unusual site still
// shows something.
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
		// Standard template → structured row.
		idTitle := tr.Find(`td[data-label="idTitle"]`)
		statusCell := tr.Find(`td[data-label="status"]`)
		if idTitle.Length() > 0 || statusCell.Length() > 0 {
			if r, ok := parseReviewRow(tr, idTitle, statusCell); ok {
				reviews = append(reviews, r)
			}
			return
		}
		// Fallback: unknown layout, keep each cell with its column header.
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

// parseReviewRow extracts one structured review from a data-labelled row.
func parseReviewRow(tr, idTitle, statusCell *goquery.Selection) (Review, bool) {
	var r Review

	// The ID/Title cell stacks two <p>s: manuscript number, then the title.
	var ps []string
	idTitle.Find("p").Each(func(_ int, p *goquery.Selection) {
		if t := clean(p.Text()); t != "" {
			ps = append(ps, t)
		}
	})
	if len(ps) > 0 {
		r.ID = ps[0]
	}
	if len(ps) > 1 {
		r.Title = strings.Join(ps[1:], " ")
	}

	r.DueDate = clean(tr.Find(`td[data-label="dueDate"]`).Text())
	r.Type = clean(tr.Find(`td[data-label="type"]`).Text())
	r.Completed = clean(tr.Find(`td[data-label="completed"]`).Text()) // Scores Submitted
	r.Sent = clean(tr.Find(`td[data-label="sent"]`).Text())           // Invitations

	// Keep any labelled cell we don't know about (queues differ per site) so an
	// unexpected column still shows up in the widget instead of vanishing.
	known := map[string]bool{"action": true, "dueDate": true, "type": true,
		"idTitle": true, "status": true, "completed": true, "sent": true}
	tr.Find(`td[data-label]`).Each(func(_ int, td *goquery.Selection) {
		label, _ := td.Attr("data-label")
		if known[label] || label == "NoResults" {
			return
		}
		if v := clean(td.Text()); v != "" {
			r.Columns = append(r.Columns, ReviewCell{Label: label, Value: v})
		}
	})

	// Status cell: the first <p> is the queue status ("Under Review", …); the
	// following <p>s — minus the "Assignments:" heading — are the handling
	// editors (SE/EIC/ME…), same people the Author page shows in <nobr>s.
	var lines []string
	statusCell.Find("p").Each(func(_ int, p *goquery.Selection) {
		if t := clean(p.Text()); t != "" {
			lines = append(lines, t)
		}
	})
	if len(lines) > 0 {
		r.Status = lines[0]
		for _, l := range lines[1:] {
			if strings.EqualFold(strings.TrimSuffix(l, ":"), "assignments") {
				continue
			}
			r.Editors = append(r.Editors, l)
		}
	}

	// The Action dropdown: each real option is something the reviewer can still
	// do ("Continue Review", "View Proof", …) and carries the manuscript number,
	// title and abstract as data attributes — used as fallbacks and for display.
	tr.Find(`td[data-label="action"] option`).Each(func(_ int, o *goquery.Selection) {
		if r.Abstract == "" {
			if a, _ := o.Attr("data-abstract"); clean(a) != "" {
				r.Abstract = clean(a)
			}
		}
		if r.ID == "" {
			if v, _ := o.Attr("data-documentno"); clean(v) != "" {
				r.ID = clean(v)
			}
		}
		if r.Title == "" {
			if v, _ := o.Attr("data-title"); clean(v) != "" {
				r.Title = clean(v)
			}
		}
		t := clean(o.Text())
		// Skip the placeholder and the "——————" separator options.
		if t == "" || strings.EqualFold(t, "Select...") || !wordRe.MatchString(t) {
			return
		}
		r.Actions = append(r.Actions, t)
	})

	ok := r.ID != "" || r.Title != "" || r.Status != "" || r.DueDate != ""
	return r, ok
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
