package tracker

// PaperFox driver — www.paperfox.ai, a modern conference-management system
// (used e.g. by CIST).
//
// PaperFox is a client-rendered Next.js app: after a plain email/password
// sign-in, /submissions and /reviews build their lists in the browser from
// follow-up API calls, so the driver waits for the rendered rows rather than a
// server page. Helpfully, the DOM is instrumented with stable data-testid
// attributes (my-submission-row-<id>, review-completed-count,
// reviewer-progress-status-N, …) — the parsers key on those.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

// retrievePaperFox logs into one PaperFox site and scrapes its Submissions and
// Reviews pages.
func retrievePaperFox(parent context.Context, c SiteCreds) SiteResult {
	res := SiteResult{Key: c.Key, Name: c.Name, URL: c.URL}
	if c.URL == "" || c.Username == "" || c.Password == "" {
		res.Error = "missing site URL, username, or password"
		return res
	}

	ctx, cleanup := newSiteBrowser(parent, 2*time.Minute)
	defer cleanup()

	err := chromedp.Run(ctx,
		chromedp.Navigate(c.URL),
		chromedp.WaitVisible(`input[type="email"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[type="email"]`, c.Username, chromedp.ByQuery),
		chromedp.SendKeys(`input[type="password"]`, c.Password, chromedp.ByQuery),
		chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
	)
	if err != nil {
		res.Error = "could not load the sign-in page (" + cleanErr(err) + ")"
		return res
	}

	switch status, msg := waitPaperFoxLogin(ctx); status {
	case "ok":
		// signed in — fall through to scraping
	case "fail":
		if msg == "" {
			msg = "the email or password was not accepted"
		}
		res.Error = "login failed: " + msg
		return res
	default: // "timeout"
		res.Error = "timed out after sign-in — the site may be slow or need extra verification"
		return res
	}

	base := siteOrigin(c.URL)
	res.Papers, res.PaperError = scrapePaperFoxSubmissions(ctx, base)
	res.Reviews, res.ReviewError = scrapePaperFoxReviews(ctx, base)
	return res
}

// waitPaperFoxLogin polls after the sign-in click. Signed-in pages carry the
// sidebar link to /submissions; the sign-in form still being there after the
// budget means the credentials were rejected. Error sniffing sticks to
// semantic hooks ([role=alert], toast nodes, the exact text-destructive class
// token) — a substring match like [class*="destructive"] would hit nearly
// every element on this Tailwind app (variant prefixes such as
// aria-invalid:ring-destructive/20 live in ordinary class strings) — and only
// counts after a few polls so a slow navigation isn't mistaken for a failure.
func waitPaperFoxLogin(ctx context.Context) (status, message string) {
	const js = `(function(){
		if (document.querySelector('a[href="/submissions"]')) return 'ok|';
		var hasPw = !!document.querySelector('input[type="password"]');
		if (!hasPw) return 'wait|';
		var el = document.querySelector('[role="alert"], [data-sonner-toast], p.text-destructive');
		var msg = el ? el.innerText.trim().split('\n')[0].replace(/\s+/g,' ').trim() : '';
		return 'login|' + msg;
	})()`
	deadline := time.Now().Add(30 * time.Second)
	sawLoginForm := false
	for poll := 0; time.Now().Before(deadline); poll++ {
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
				if m := strings.TrimPrefix(out, "login|"); m != "" && poll >= 3 {
					return "fail", clean(m)
				}
			}
		} else if ctx.Err() != nil {
			break
		}
		time.Sleep(time.Second)
	}
	if sawLoginForm {
		return "fail", ""
	}
	return "timeout", ""
}

// getPaperFoxPage navigates to one signed-in page and waits until either a row
// matching rowSel has rendered or the page's empty-state text (emptyRe,
// case-insensitive) has — the lists arrive from client-side fetches after the
// page itself loads. Gives up quietly after a short budget.
func getPaperFoxPage(ctx context.Context, pageURL, rowSel, emptyRe string) (string, error) {
	js := fmt.Sprintf(`(function(){
		if (document.querySelector(%q)) return true;
		var m = document.querySelector('main');
		return new RegExp(%q, 'i').test(m ? m.innerText : '');
	})()`, rowSel, emptyRe)
	sub, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var html string
	err := chromedp.Run(sub,
		chromedp.Navigate(pageURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			for i := 0; i < 40; i++ {
				var ready bool
				s, c2 := context.WithTimeout(ctx, 3*time.Second)
				err := chromedp.Run(s, chromedp.Evaluate(js, &ready))
				c2()
				if err == nil && ready {
					return nil
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				time.Sleep(700 * time.Millisecond)
			}
			return nil // parse whatever rendered
		}),
		chromedp.OuterHTML(`html`, &html, chromedp.ByQuery),
	)
	return html, err
}

// scrapePaperFoxSubmissions reads the My Submissions list, then each paper's
// detail page for what the list doesn't show: the paper type and the live
// review status (decision state, completed/pending counts, per-reviewer
// progress).
func scrapePaperFoxSubmissions(ctx context.Context, base string) ([]Paper, string) {
	html, err := getPaperFoxPage(ctx, base+"/submissions",
		`[data-testid^="my-submission-row-"]`, `no submissions|don't have any`)
	if err != nil {
		return nil, "could not open the Submissions page (" + cleanErr(err) + ")"
	}
	papers, paths := parsePaperFoxSubmissions(html)
	for i, path := range paths {
		if path == "" || i >= pcsDetailCap {
			continue
		}
		// The detail page is ready once its review-status card rendered.
		dhtml, derr := getPaperFoxPage(ctx, base+path,
			`[data-testid="review-completed-count"]`, `review status`)
		if derr != nil {
			continue // best-effort: the list row alone is still useful
		}
		fillPaperFoxDetail(&papers[i], dhtml)
	}
	return papers, ""
}

// parsePaperFoxSubmissions turns the rendered list into papers. Rows are
// grouped under a conference-name heading (an /conferences/<id> link); each
// row carries the submission id in its data-testid, the full title as an
// /submissions/<id> link, and small meta spans (submitted date, "0/2 reviews").
func parsePaperFoxSubmissions(html string) ([]Paper, []string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, nil
	}
	var papers []Paper
	var paths []string
	conference := ""
	doc.Find(`main a[href^="/conferences/"], main div[data-testid^="my-submission-row-"]`).Each(
		func(_ int, s *goquery.Selection) {
			if goquery.NodeName(s) == "a" {
				conference = clean(s.Text())
				return
			}
			testid, _ := s.Attr("data-testid")
			p := Paper{
				ID:       strings.TrimPrefix(testid, "my-submission-row-"),
				Category: conference,
			}
			link := s.Find(`a[href^="/submissions/"]`).First()
			p.Title = clean(link.Text())
			path, _ := link.Attr("href")
			// Meta lives in small spans (submitted date, "0/2 reviews"). Read them
			// span-by-span — adjacent text nodes concatenate without whitespace, so
			// a whole-row regex would bleed the date into the review count.
			s.Find("span").Each(func(_ int, sp *goquery.Selection) {
				t := clean(sp.Text())
				if len(t) > 24 {
					return
				}
				if p.Submitted == "" && dateRe.MatchString(t) {
					p.Submitted = dateRe.FindString(t)
				}
				if p.Note == "" && reviewsRe.MatchString(t) {
					p.Note = reviewsRe.FindString(t)
				}
			})
			if p.ID == "" && p.Title == "" {
				return
			}
			papers = append(papers, p)
			paths = append(paths, path)
		})
	return papers, paths
}

// fillPaperFoxDetail enriches one paper from its detail page:
//   - Section  ← the "Paper Type" card (e.g. "Complete") — shown as a badge;
//   - Status   ← the decision line of the Review Status card ("Under review",
//     or the decision once one is made);
//   - Note     ← completed/pending counts plus each reviewer's progress.
func fillPaperFoxDetail(p *Paper, html string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return
	}
	// "Paper Type" card: an <h4> heading with the value in the next <p>.
	doc.Find("main h4").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		if !strings.EqualFold(clean(h.Text()), "Paper Type") {
			return true
		}
		if v := clean(h.NextAllFiltered("p").First().Text()); v != "" {
			p.Section = v
		}
		return false
	})
	// Decision state ("Under review" until a decision is made).
	if v := clean(doc.Find(`[data-testid="no-decision-message"]`).First().Text()); v != "" {
		p.Status = v
	} else if v := clean(doc.Find(`[data-testid*="decision-message"]`).First().Text()); v != "" {
		p.Status = v
	}
	// Review progress: counts + per-reviewer state.
	var bits []string
	if v := clean(doc.Find(`[data-testid="review-completed-count"]`).First().Text()); v != "" {
		bits = append(bits, v+" completed")
	}
	if v := clean(doc.Find(`[data-testid="review-pending-count"]`).First().Text()); v != "" {
		bits = append(bits, v+" pending")
	}
	doc.Find(`[data-testid^="reviewer-progress-row-"]`).Each(func(i int, row *goquery.Selection) {
		name := clean(row.Find(`[data-testid^="reviewer-progress-name-"]`).Text())
		state := clean(row.Find(`[data-testid^="reviewer-progress-status-"]`).Text())
		if name != "" && state != "" {
			bits = append(bits, name+": "+state)
		}
	})
	if len(bits) > 0 {
		p.Note = strings.Join(bits, " · ")
	}
}

// scrapePaperFoxReviews reads the Reviews page. Only the empty state has been
// observed so far ("No Assignments Yet"); a populated list is parsed
// best-effort — rows with a review-ish data-testid, else any /reviews/<id>
// links — so assignments surface rather than vanish even if the exact layout
// differs.
func scrapePaperFoxReviews(ctx context.Context, base string) ([]Review, string) {
	html, err := getPaperFoxPage(ctx, base+"/reviews",
		`[data-testid^="my-review-row-"], [data-testid*="assignment-row"]`,
		`no assignments|don't have any`)
	if err != nil {
		return nil, "could not open the Reviews page (" + cleanErr(err) + ")"
	}
	return parsePaperFoxReviews(html), ""
}

func parsePaperFoxReviews(html string) []Review {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var reviews []Review
	seen := map[string]bool{}
	// Preferred: instrumented rows (shape assumed to mirror the submissions list).
	doc.Find(`main [data-testid^="my-review-row-"], main [data-testid*="assignment-row"]`).Each(
		func(_ int, s *goquery.Selection) {
			testid, _ := s.Attr("data-testid")
			r := Review{Queue: "Assignments"}
			r.ID = strings.TrimPrefix(testid, "my-review-row-")
			link := s.Find(`a[href^="/reviews/"], a[href^="/submissions/"]`).First()
			r.Title = clean(link.Text())
			if r.Title == "" {
				r.Title = clean(s.Text())
			}
			s.Find("span").Each(func(_ int, sp *goquery.Selection) {
				t := clean(sp.Text())
				if r.DueDate == "" && len(t) <= 24 && dateRe.MatchString(t) {
					r.DueDate = dateRe.FindString(t)
				}
			})
			if r.Title == "" && r.ID == "" {
				return
			}
			seen[r.ID] = true
			reviews = append(reviews, r)
		})
	if len(reviews) > 0 {
		return reviews
	}
	// Fallback: any assignment links in the main content.
	doc.Find(`main a[href^="/reviews/"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		title := clean(a.Text())
		if title == "" || seen[href] {
			return
		}
		seen[href] = true
		reviews = append(reviews, Review{
			Queue: "Assignments",
			ID:    strings.TrimPrefix(href, "/reviews/"),
			Title: title,
		})
	})
	return reviews
}
