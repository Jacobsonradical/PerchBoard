package tracker

// PCS (Precision Conference Solutions) driver — new.precisionconference.com,
// the system behind conferences like ICIS and the SIGCHI family.
//
// PCS is much simpler than ScholarOne: a plain POST login form
// (#username/#password) and ordinary links to /submissions and /reviews. The
// one catch is the tables — PCS renders an empty DataTables skeleton and fills
// the rows with a follow-up AJAX call, so after each navigation we wait until
// every dynamic table on the page has rows (DataTables puts a "No data
// available" row into an empty table, so populated and empty both finish the
// wait).

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

// retrievePCS logs into one PCS site and scrapes its Submissions and Reviews
// pages.
func retrievePCS(parent context.Context, c SiteCreds) SiteResult {
	res := SiteResult{Key: c.Key, Name: c.Name, URL: c.URL}
	if c.URL == "" || c.Username == "" || c.Password == "" {
		res.Error = "missing site URL, username, or password"
		return res
	}

	ctx, cleanup := newSiteBrowser(parent, 2*time.Minute)
	defer cleanup()

	// Open the login page and submit the credentials.
	err := chromedp.Run(ctx,
		chromedp.Navigate(c.URL),
		chromedp.WaitVisible(`#username`, chromedp.ByQuery),
		chromedp.SendKeys(`#username`, c.Username, chromedp.ByQuery),
		chromedp.SendKeys(`#password`, c.Password, chromedp.ByQuery),
		chromedp.Click(`form input[type="submit"]`, chromedp.ByQuery),
	)
	if err != nil {
		res.Error = "could not load the login page (" + cleanErr(err) + ")"
		return res
	}

	switch status, msg := waitPCSLogin(ctx); status {
	case "ok":
		// logged in — fall through to scraping
	case "fail":
		if msg == "" {
			msg = "the username or password was not accepted"
		}
		res.Error = "login failed: " + msg
		return res
	default: // "timeout"
		res.Error = "timed out after login — the site may be slow or need extra verification"
		return res
	}

	base := siteOrigin(c.URL)
	res.Papers, res.PaperError = scrapePCSSubmissions(ctx, base)
	res.Reviews, res.ReviewError = scrapePCSPage(ctx, base+"/reviews", "Reviews page", parsePCSReviews)
	return res
}

// pcsDetailCap bounds how many per-submission detail pages one retrieval will
// open (each is an extra navigation; nobody has this many live submissions).
const pcsDetailCap = 12

// scrapePCSSubmissions reads the Submissions page, then follows each row's
// "See submission" link: the table truncates long titles ("Political Bias
// in ..."), and only the detail page carries the full one.
func scrapePCSSubmissions(ctx context.Context, base string) ([]Paper, string) {
	html, err := getPCSPage(ctx, base+"/submissions")
	if err != nil {
		return nil, "could not open the Submissions page (" + cleanErr(err) + ")"
	}
	papers, paths := parsePCSSubmissions(html)
	for i, path := range paths {
		if path == "" || i >= pcsDetailCap {
			continue
		}
		dhtml, derr := getPCSPage(ctx, base+path)
		if derr != nil {
			continue // best-effort: the truncated title is still usable
		}
		if title := parsePCSDetailTitle(dhtml); title != "" {
			papers[i].Title = title
		}
	}
	return papers, ""
}

// waitPCSLogin polls after the Sign in click until the account navigation
// shows up ("ok"), the login form re-renders with an error ("fail"), or time
// runs out. PCS keeps the /submissions & /reviews menu links on every
// signed-in page, so their presence is the login signal.
func waitPCSLogin(ctx context.Context) (status, message string) {
	const js = `(function(){
		if (document.querySelector('a[href="/submissions"]')) return 'ok|';
		var hasPw = !!document.querySelector('#password');
		if (!hasPw) return 'wait|';
		// Still on the login form. Report any error/flash message the page shows
		// (first line only — nested wrappers repeat the same text).
		var el = document.querySelector('.flash, .error, .errors, [class*="alert"]');
		var msg = el ? el.innerText.trim().split('\n')[0].replace(/\s+/g,' ').trim() : '';
		return 'login|' + msg;
	})()`
	deadline := time.Now().Add(30 * time.Second)
	sawLoginForm := false
	loginMsg := ""
	for time.Now().Before(deadline) {
		var out string
		sub, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := chromedp.Run(sub, chromedp.Evaluate(js, &out))
		cancel()
		if err == nil {
			switch {
			case strings.HasPrefix(out, "ok|"):
				return "ok", ""
			case strings.HasPrefix(out, "login|"):
				sawLoginForm = true
				if m := strings.TrimPrefix(out, "login|"); m != "" {
					// An explicit error message — no need to keep waiting.
					return "fail", clean(m)
				}
			}
		} else if ctx.Err() != nil {
			break
		}
		time.Sleep(time.Second)
	}
	// The form never went away: the server re-rendered the login page, which is
	// what a rejected password looks like even without an error banner.
	if sawLoginForm {
		return "fail", loginMsg
	}
	return "timeout", ""
}

// scrapePCSPage opens one signed-in PCS page, waits for its dynamic tables to
// fill, and parses the HTML.
func scrapePCSPage[T any](ctx context.Context, pageURL, what string, parse func(string) []T) ([]T, string) {
	html, err := getPCSPage(ctx, pageURL)
	if err != nil {
		return nil, "could not open the " + what + " (" + cleanErr(err) + ")"
	}
	return parse(html), ""
}

// getPCSPage navigates to one signed-in page, waits for its dynamic tables to
// receive their AJAX rows, and returns the rendered HTML.
func getPCSPage(ctx context.Context, pageURL string) (string, error) {
	sub, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var html string
	err := chromedp.Run(sub,
		chromedp.Navigate(pageURL),
		waitPCSTables(),
		chromedp.OuterHTML(`html`, &html, chromedp.ByQuery),
	)
	return html, err
}

// waitPCSTables waits until every dynamic table on the page has received its
// AJAX rows (an empty table gets a "No data available" row, so this terminates
// either way; a page with no dynamic tables is ready immediately). Gives up
// quietly after a short budget so the caller still parses what rendered.
func waitPCSTables() chromedp.Action {
	const js = `(function(){
		if (document.readyState !== 'complete') return false;
		var ts = document.querySelectorAll('table.dynamictable');
		for (var i = 0; i < ts.length; i++) {
			if (!ts[i].querySelector('tbody tr')) return false;
		}
		return true;
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
		return nil
	})
}

// pcsSections walks a page's <main> in document order and yields each dynamic
// table together with the closest preceding <h2> title ("Existing
// Submissions", "Past Submissions", "Reviews in Progress", …).
func pcsSections(doc *goquery.Document, fn func(section string, table *goquery.Selection)) {
	section := ""
	doc.Find("main h2, main table.dynamictable").Each(func(_ int, s *goquery.Selection) {
		if goquery.NodeName(s) == "h2" {
			section = clean(s.Text())
			return
		}
		fn(section, s)
	})
}

// pcsHeaders returns a table's column keys, normalized to lowercase
// alphanumerics ("Submission<br>Deadline" → "submissiondeadline") so matching
// is immune to line breaks, spacing, and case.
func pcsHeaders(table *goquery.Selection) []string {
	var keys []string
	table.Find("thead th").Each(func(_ int, th *goquery.Selection) {
		k := strings.ToLower(clean(th.Text()))
		k = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, k)
		keys = append(keys, k)
	})
	return keys
}

// pcsRow reads one body row into header-keyed cells, the row's link texts, and
// the row's link hrefs. Returns nils for DataTables' "No data available"
// placeholder row.
func pcsRow(keys []string, tr *goquery.Selection) (map[string]string, map[string][]string, map[string][]string) {
	if tr.Find("td.dataTables_empty").Length() > 0 {
		return nil, nil, nil
	}
	cells := map[string]string{}
	links := map[string][]string{}
	hrefs := map[string][]string{}
	empty := true
	tr.Find("td").Each(func(i int, td *goquery.Selection) {
		if i >= len(keys) {
			return
		}
		v := clean(td.Text())
		if v != "" {
			empty = false
		}
		cells[keys[i]] = v
		td.Find("a").Each(func(_ int, a *goquery.Selection) {
			if t := clean(a.Text()); t != "" {
				links[keys[i]] = append(links[keys[i]], t)
				h, _ := a.Attr("href")
				hrefs[keys[i]] = append(hrefs[keys[i]], h)
			}
		})
	})
	if empty {
		return nil, nil, nil
	}
	return cells, links, hrefs
}

// parsePCSSubmissions reads the Submissions page. Known columns: Submission
// Deadline / Status / Title / Actions / Note / ID / Category; the "Existing
// Submissions" table is the main list (Section left empty), any other section
// (e.g. "Past Submissions") is named on its rows. The second return value is
// each paper's detail-page path (from its "See submission" action link, when
// present), aligned by index.
func parsePCSSubmissions(html string) ([]Paper, []string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, nil
	}
	var papers []Paper
	var paths []string
	pcsSections(doc, func(section string, table *goquery.Selection) {
		keys := pcsHeaders(table)
		table.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
			cells, links, hrefs := pcsRow(keys, tr)
			if cells == nil {
				return
			}
			p := Paper{
				ID:       cells["id"],
				Title:    cells["title"],
				Status:   cells["status"],
				Deadline: cells["submissiondeadline"],
				Category: cells["category"],
				Note:     cells["note"],
				Actions:  links["actions"],
			}
			if p.Actions == nil && cells["actions"] != "" {
				p.Actions = []string{cells["actions"]}
			}
			if !strings.EqualFold(section, "Existing Submissions") {
				p.Section = section
			}
			if p.ID == "" && p.Title == "" && p.Status == "" {
				return
			}
			// The submission's own page — prefer the "See submission" link.
			path := ""
			for i, t := range links["actions"] {
				h := hrefs["actions"][i]
				if !strings.HasPrefix(h, "/") {
					continue
				}
				if strings.Contains(strings.ToLower(t), "submission") || path == "" {
					path = h
				}
			}
			papers = append(papers, p)
			paths = append(paths, path)
		})
	})
	return papers, paths
}

// parsePCSDetailTitle pulls the full paper title from a submission's detail
// page, which lays fields out as an <h2> label followed by a .formItem value.
func parsePCSDetailTitle(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	title := ""
	doc.Find("main h2").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		if !strings.EqualFold(clean(h.Text()), "Paper Title") {
			return true
		}
		title = clean(h.NextAllFiltered("div.formItem").First().Text())
		return false
	})
	return title
}

// parsePCSReviews reads the Reviews page. Populated review tables haven't been
// observed yet, so the mapping is by common column names with everything
// unrecognized kept in Columns; the section title ("Reviews in Progress", …)
// becomes the Queue, mirroring the ScholarOne grouping.
func parsePCSReviews(html string) []Review {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	known := map[string]bool{
		"id": true, "submissionid": true, "title": true, "status": true,
		"duedate": true, "due": true, "completed": true, "type": true,
		"actions": true,
	}
	var reviews []Review
	pcsSections(doc, func(section string, table *goquery.Selection) {
		keys := pcsHeaders(table)
		table.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
			cells, links, _ := pcsRow(keys, tr)
			if cells == nil {
				return
			}
			r := Review{
				Queue:     section,
				ID:        cells["id"],
				Title:     cells["title"],
				Status:    cells["status"],
				Type:      cells["type"],
				Completed: cells["completed"],
				Actions:   links["actions"],
			}
			if r.ID == "" {
				r.ID = cells["submissionid"]
			}
			if r.DueDate = cells["duedate"]; r.DueDate == "" {
				r.DueDate = cells["due"]
			}
			if r.Actions == nil && cells["actions"] != "" {
				r.Actions = []string{cells["actions"]}
			}
			for _, k := range keys {
				if !known[k] && cells[k] != "" {
					r.Columns = append(r.Columns, ReviewCell{Label: k, Value: cells[k]})
				}
			}
			if r.ID == "" && r.Title == "" && r.Status == "" && len(r.Columns) == 0 {
				return
			}
			reviews = append(reviews, r)
		})
	})
	return reviews
}

// siteOrigin reduces a site URL to its scheme://host origin, so signed-in
// pages can be addressed from the login URL.
func siteOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	return u.Scheme + "://" + u.Host
}
