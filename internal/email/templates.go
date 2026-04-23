package email

import (
	"bytes"
	"fmt"
	"html/template"
	"log"

	"github.com/jmoiron/sqlx"
)

// Branding holds per-tenant email branding, loaded from the email_templates table.
type Branding struct {
	BrandName       string `db:"brand_name"`
	BrandAddress    string `db:"brand_address"`
	LogoURL         string `db:"logo_url"`
	PrimaryColor    string `db:"primary_color"`
	SecondaryColor  string `db:"secondary_color"`
	GoogleReviewURL string `db:"google_review_url"`
}

// DefaultBranding returns Falooda & Co branding as the fallback.
func DefaultBranding() Branding {
	return Branding{
		BrandName:       "Falooda & Co",
		BrandAddress:    "268 Farnham Rd, Slough SL1 4XJ",
		LogoURL:         "",
		PrimaryColor:    "#ec4899",
		SecondaryColor:  "#f97316",
		GoogleReviewURL: "https://share.google/DlwFbPHWWJeln3Stt",
	}
}

// LoadBranding retrieves tenant-specific email branding from the database.
// Falls back to Falooda & Co defaults if no template exists for the store.
func LoadBranding(db *sqlx.DB, storeID string) Branding {
	if db == nil || storeID == "" {
		return DefaultBranding()
	}

	var b Branding
	err := db.Get(&b, `
		SELECT brand_name, COALESCE(brand_address, '') as brand_address,
		       COALESCE(logo_url, '') as logo_url,
		       COALESCE(primary_color, '#ec4899') as primary_color,
		       COALESCE(secondary_color, '#f97316') as secondary_color,
		       COALESCE(google_review_url, '') as google_review_url
		FROM email_templates WHERE store_id = $1 LIMIT 1
	`, storeID)
	if err != nil {
		return DefaultBranding()
	}
	return b
}

// ─── Email Templates ───

const rewardEmailTpl = `
<div style="font-family:'Segoe UI',Arial,sans-serif;max-width:500px;margin:0 auto;background:#fafafa;border-radius:12px;overflow:hidden;">
	<div style="background:linear-gradient(135deg,{{.PrimaryColor}},{{.SecondaryColor}});padding:24px 32px;color:white;text-align:center;">
		<h1 style="margin:0;font-size:24px;">🎉 You've Earned a Reward!</h1>
	</div>
	<div style="padding:24px 32px;text-align:center;">
		<p style="font-size:16px;color:#374151;">Hi {{.CustomerName}},</p>
		<p style="font-size:18px;font-weight:600;color:#1f2937;">{{.RewardDescription}}</p>
		<p style="font-size:14px;color:#6b7280;">Show this email at the counter or mention your phone number to redeem.</p>
		<div style="margin:24px 0;padding:16px;background:linear-gradient(135deg,#fef3c7,#fde68a);border-radius:8px;">
			<p style="margin:0;font-size:14px;font-weight:600;color:#92400e;">Valid for 30 days from today</p>
		</div>
	</div>
	<div style="padding:12px 32px;background:#f3f4f6;text-align:center;font-size:11px;color:#9ca3af;">
		{{.BrandName}} • {{.BrandAddress}}
	</div>
</div>`

const winbackEmailTpl = `
<div style="font-family:'Segoe UI',Arial,sans-serif;max-width:500px;margin:0 auto;background:#fafafa;border-radius:12px;overflow:hidden;">
	<div style="background:linear-gradient(135deg,{{.PrimaryColor}},#3b82f6);padding:24px 32px;color:white;text-align:center;">
		<h1 style="margin:0;font-size:24px;">Come Back for Dessert!</h1>
	</div>
	<div style="padding:24px 32px;text-align:center;">
		<p style="font-size:16px;color:#374151;">Hi {{.CustomerName}},</p>
		<p style="font-size:16px;color:#374151;">We noticed you haven't visited {{.BrandName}} in a while. We would love to see you again!</p>
		<p style="font-size:18px;font-weight:600;color:#1f2937;">{{.OfferDescription}}</p>
		<div style="margin:24px 0;padding:16px;background:linear-gradient(135deg,#fef3c7,#fde68a);border-radius:8px;">
			<p style="margin:0;font-size:14px;font-weight:600;color:#92400e;">Show this email at the counter to redeem. Valid for 7 days.</p>
		</div>
	</div>
	<div style="padding:12px 32px;background:#f3f4f6;text-align:center;font-size:11px;color:#9ca3af;">
		{{.BrandName}} • {{.BrandAddress}}
	</div>
</div>`

const reviewEmailTpl = `
<div style="font-family:'Segoe UI',Arial,sans-serif;max-width:500px;margin:0 auto;background:#fafafa;border-radius:12px;overflow:hidden;">
	<div style="background:linear-gradient(135deg,#1a1a2e,#16213e);padding:28px 32px;color:white;text-align:center;">
		<h1 style="margin:0;font-size:26px;">Thank You, {{.CustomerName}}! 🙏</h1>
		<p style="margin:8px 0 0;opacity:0.8;font-size:14px;">We hope you enjoyed your experience at {{.BrandName}}</p>
	</div>
	<div style="padding:28px 32px;text-align:center;">
		<p style="font-size:15px;color:#374151;line-height:1.6;">
			Your support means the world to us! If you had a great time,
			we'd love to hear about it. A quick review helps other food
			lovers discover us.
		</p>
		<a href="{{.ReviewURL}}"
		   style="display:inline-block;margin:20px 0;padding:14px 36px;background:linear-gradient(135deg,{{.SecondaryColor}},{{.PrimaryColor}});color:white;text-decoration:none;border-radius:8px;font-size:16px;font-weight:600;letter-spacing:0.5px;">
			⭐ Leave a Google Review
		</a>
		<p style="font-size:12px;color:#9ca3af;margin-top:16px;">
			It only takes 30 seconds and makes a huge difference!
		</p>
	</div>
	<div style="padding:16px 32px;background:#f3f4f6;text-align:center;">
		<p style="margin:0;font-size:11px;color:#9ca3af;">
			{{.BrandName}} • {{.BrandAddress}}<br>
			You're receiving this because you recently ordered from us
		</p>
	</div>
</div>`

const campaignEmailTpl = `
<div style="font-family:'Segoe UI',Arial,sans-serif;max-width:500px;margin:0 auto;background:#fafafa;border-radius:12px;overflow:hidden;">
	<div style="background:linear-gradient(135deg,{{.PrimaryColor}},{{.SecondaryColor}});padding:28px 32px;color:white;text-align:center;">
		<h1 style="margin:0;font-size:24px;">{{.Headline}}</h1>
	</div>
	<div style="padding:28px 32px;text-align:center;">
		<p style="font-size:16px;color:#374151;">Hi {{.CustomerName}},</p>
		<p style="font-size:18px;font-weight:600;color:#1f2937;">{{.OfferDescription}}</p>
		{{if .OfferCode}}
		<div style="margin:24px 0;padding:16px;background:linear-gradient(135deg,#fef3c7,#fde68a);border-radius:8px;">
			<p style="margin:0;font-size:14px;font-weight:600;color:#92400e;">Use code: {{.OfferCode}}</p>
		</div>
		{{end}}
	</div>
	<div style="padding:12px 32px;background:#f3f4f6;text-align:center;font-size:11px;color:#9ca3af;">
		{{.BrandName}} • {{.BrandAddress}}
	</div>
</div>`

// ─── Render Functions ───

type RewardEmailData struct {
	PrimaryColor     string
	SecondaryColor   string
	CustomerName     string
	RewardDescription string
	BrandName        string
	BrandAddress     string
}

type WinBackEmailData struct {
	PrimaryColor   string
	CustomerName   string
	OfferDescription string
	BrandName      string
	BrandAddress   string
}

type ReviewEmailData struct {
	PrimaryColor   string
	SecondaryColor string
	CustomerName   string
	ReviewURL      string
	BrandName      string
	BrandAddress   string
}

type CampaignEmailData struct {
	PrimaryColor     string
	SecondaryColor   string
	CustomerName     string
	Headline         string
	OfferDescription string
	OfferCode        string
	BrandName        string
	BrandAddress     string
}

func RenderRewardEmail(b Branding, customerName, rewardDesc string) string {
	return renderTemplate(rewardEmailTpl, RewardEmailData{
		PrimaryColor:     b.PrimaryColor,
		SecondaryColor:   b.SecondaryColor,
		CustomerName:     customerName,
		RewardDescription: rewardDesc,
		BrandName:        b.BrandName,
		BrandAddress:     b.BrandAddress,
	})
}

func RenderWinBackEmail(b Branding, customerName, offerDesc string) string {
	return renderTemplate(winbackEmailTpl, WinBackEmailData{
		PrimaryColor:   b.PrimaryColor,
		CustomerName:   customerName,
		OfferDescription: offerDesc,
		BrandName:      b.BrandName,
		BrandAddress:   b.BrandAddress,
	})
}

func RenderReviewEmail(b Branding, customerName string) string {
	reviewURL := b.GoogleReviewURL
	if reviewURL == "" {
		reviewURL = "https://share.google/DlwFbPHWWJeln3Stt" // Falooda default
	}
	return renderTemplate(reviewEmailTpl, ReviewEmailData{
		PrimaryColor:   b.PrimaryColor,
		SecondaryColor: b.SecondaryColor,
		CustomerName:   customerName,
		ReviewURL:      reviewURL,
		BrandName:      b.BrandName,
		BrandAddress:   b.BrandAddress,
	})
}

func RenderCampaignEmail(b Branding, customerName, headline, offerDesc, offerCode string) string {
	return renderTemplate(campaignEmailTpl, CampaignEmailData{
		PrimaryColor:     b.PrimaryColor,
		SecondaryColor:   b.SecondaryColor,
		CustomerName:     customerName,
		Headline:         headline,
		OfferDescription: offerDesc,
		OfferCode:        offerCode,
		BrandName:        b.BrandName,
		BrandAddress:     b.BrandAddress,
	})
}

// renderTemplate safely parses and executes a Go html/template, returning the rendered HTML string.
func renderTemplate(tplStr string, data interface{}) string {
	tpl, err := template.New("email").Parse(tplStr)
	if err != nil {
		log.Printf("Email template parse error: %v", err)
		return fmt.Sprintf("<p>Error rendering email template</p>")
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		log.Printf("Email template execution error: %v", err)
		return fmt.Sprintf("<p>Error rendering email template</p>")
	}
	return buf.String()
}
