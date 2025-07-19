package telegram

import "fmt"

// Common message templates and constants for user-facing messages

const (
	// Upgrade and limit messages
	UpgradeCommandHint = "Use /coffee to upgrade your plan for higher limits!"
	UpgradePremiumHint = "Use /coffee to upgrade to premium for higher limits"
	UpgradeForMoreHint = "Use /coffee to upgrade for more"
	
	// Repository capacity messages
	RepoAlmostFullTemplate = `🟡 <b>Repository almost full</b>

Your repository is at <b>%.1f%%</b> capacity. 

Cannot add more content when repository is nearly full. Please:
• Clean up your repository to free space
• Use /coffee to upgrade to premium for higher limits

<i>Note: You can still read existing content with commands like /todo and /issue</i>`

	RepoCapacityLimitSimple = "❌ Repository capacity limit reached. Use /coffee to upgrade."
	
	RepoCapacityIssueTemplate = `🚫 <b>Repository capacity exceeded</b>

Your repository is at <b>%.1f%%</b> capacity (%.2f MB / %.2f MB).

Cannot create new GitHub issues when repository is full. Please:
• Clean up your repository to free space
• Use /coffee to upgrade to premium for higher limits

<i>Note: You can still view existing issues with /issue command</i>`
	
	RepoPhotoUploadLimitTemplate = `🟡 <b>Repository almost full</b>

Your repository is at <b>%.1f%%</b> capacity. 

Cannot upload photos when repository is nearly full. Please:
• Clean up your repository to free space
• Use /coffee to upgrade to premium for higher limits

<i>Note: You can still read existing content with commands like /todo and /issue</i>`

	// Custom file limit messages
	CustomFileLimitReachedTemplate = "❌ Custom file limit reached!\n\n%s tier: %d custom files maximum\n\nUpgrade with /coffee for more custom files!"
	
	// Image limit messages
	ImageLimitReachedTemplate = "❌ Image limit reached (%d/%d). Use /coffee to upgrade."
	ImageLimitReachedDetailedTemplate = `📸 <b>Image upload limit reached</b>

You've used <b>%d/%d images</b> on the %s tier.%s

Use /coffee to upgrade your plan for higher limits!

<i>Note: You can still save text messages and read existing content</i>`
	
	// Tier upgrade hints with specific benefits
	TierUpgradeHintTemplate = "\n\n⚠️ You've reached the %s tier limit (%d %s). Use /coffee to upgrade and get up to %d %s!"
	
	// Issue limit messages
	IssueLimitUpgradeTemplate = "\n\nUse /coffee to upgrade and get up to %d issues!"
	
	// Common upgrade benefits message
	UpgradeBenefitsTemplate = `💡 Upgrade to %s tier to get %s!

Use /coffee to upgrade your plan for higher limits!

Note: You can still save text messages and read existing content`

	// Premium access required
	PremiumAccessRequired = "❌ This feature requires premium access. Use /coffee to upgrade and support the project!"
	
	// Payment cancelled message
	PaymentCancelledMessage = "❌ Payment cancelled. You can always upgrade later with /coffee"
	
	// General upgrade tip
	UpgradeTipMessage = "\n\n💡 <b>Tip:</b> Use /coffee to upgrade for higher limits!"
	
	// Repository setup error with upgrade hint
	RepoSetupUpgradeHint = "\n💡 <i>Upgrade with /coffee for more space!</i>"
)

// Tier names for consistent display
var TierNames = map[int]string{
	0: "Free",
	1: "☕ Coffee", 
	2: "🍰 Cake",
	3: "🎁 Sponsor",
}

// GetTierName returns the display name for a premium level
func GetTierName(premiumLevel int) string {
	if name, exists := TierNames[premiumLevel]; exists {
		return name
	}
	return "Free"
}

// FormatCustomFileLimitMessage formats the custom file limit reached message
func FormatCustomFileLimitMessage(premiumLevel, currentLimit int) string {
	tierName := GetTierName(premiumLevel)
	return fmt.Sprintf(CustomFileLimitReachedTemplate, tierName, currentLimit)
}

// FormatTierUpgradeHint formats the tier upgrade hint message
func FormatTierUpgradeHint(premiumLevel, currentLimit, nextLimit int, itemType string) string {
	tierName := GetTierName(premiumLevel)
	return fmt.Sprintf(TierUpgradeHintTemplate, tierName, currentLimit, itemType, nextLimit, itemType)
}

// FormatUpgradeBenefits formats the upgrade benefits message with specific tier and benefits
func FormatUpgradeBenefits(tierName, benefits string) string {
	return fmt.Sprintf(UpgradeBenefitsTemplate, tierName, benefits)
}