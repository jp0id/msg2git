package consts

// File Types and Extensions
const (
	FileTypeNote   = "NOTE"
	FileTypeTodo   = "TODO"
	FileTypeIssue  = "ISSUE"
	FileTypeIdea   = "IDEA"
	FileTypeInbox  = "INBOX"
	FileTypeTool   = "TOOL"
	FileTypeCustom = "CUSTOM"

	FileNameNote  = "note.md"
	FileNameTodo  = "todo.md"
	FileNameIssue = "issue.md"
	FileNameIdea  = "idea.md"
	FileNameInbox = "inbox.md"
	FileNameTool  = "tool.md"
)

// Button Labels with Emojis
const (
	ButtonNote   = "📝 NOTE"
	ButtonTodo   = "✅ TODO"
	ButtonIssue  = "❓ ISSUE"
	ButtonIdea   = "💡 IDEA"
	ButtonInbox  = "📥 INBOX"
	ButtonTool   = "🔧 TOOL"
	ButtonCustom = "📁 CUSTOM"
	ButtonCancel = "❌ CANCEL"

	ButtonAddNewFile = "➕ Add New File"
	ButtonRemoveFile = "🗑️ Remove File"
	ButtonDone       = "✅ Done"
	ButtonBack       = "🔙 Back"
	ButtonMore       = "📋 Show More"
	ButtonRefresh    = "🔄 Refresh"

	ButtonCoffee             = "☕ Coffee $5"
	ButtonCake               = "🍰 Cake $15"
	ButtonSponsor            = "🎁 Sponsor $50"
	ButtonReset              = "🔄 Usage Reset"
	ButtonManageSubscription = "⚙️ Manage Subscription"

	ButtonSetRepo      = "📁 Choose Repo"
	ButtonSetRepoToken = "🔑 Manually Auth"
	ButtonSetCommitter = "👤 Committer"
	ButtonGitHubOAuth  = "🔐 GitHub OAuth"
	ButtonRevokeAuth   = "🚫 Revoke Auth"
	ButtonOAuthCancel  = "❌ Cancel"
)

// Premium Tier Information
const (
	TierFree    = "Free"
	TierCoffee  = "☕ Coffee"
	TierCake    = "🍰 Cake"
	TierSponsor = "🎁 Sponsor"

	PriceCoffee  = 6.0
	PriceCake    = 12.0
	PriceSponsor = 30.0
	PriceReset   = 1.00

	PremiumLevelFree    = 0
	PremiumLevelCoffee  = 1
	PremiumLevelCake    = 2
	PremiumLevelSponsor = 3

	MultiplierFree    = 1
	MultiplierCoffee  = 2
	MultiplierCake    = 4
	MultiplierSponsor = 10

	DurationYears    = "1 year"
	DurationLifetime = "lifetime"
)

// Common Error Messages
const (
	ErrorDatabaseNotConfigured = "❌ Database not configured"
	ErrorUserNotFound          = "❌ User not found"
	ErrorGitHubNotConfigured   = "❌ GitHub not configured. Please set up with /repo command"
	ErrorPremiumRequired       = "❌ This feature requires premium access. Use /coffee to upgrade!"
	ErrorRepositorySetupFailed = "❌ Repository setup failed"
	ErrorCapacityExceeded      = "❌ Repository capacity exceeded"
	ErrorLimitReached          = "❌ Limit reached"
	ErrorInvalidFormat         = "❌ Invalid format"
	ErrorOperationFailed       = "❌ Operation failed"
	ErrorAuthorizationFailed   = "❌ Authorization failed"
	ErrorFileNotFound          = "❌ File not found"
	ErrorCustomFileExists      = "⚠️ Custom file already exists!"
	ErrorEmptyInput            = "❌ Input cannot be empty"
	ErrorInvalidPath           = "❌ Invalid file path"
	ErrorTodoLineBreaks        = "❌ TODOs cannot contain line breaks. Please use a different file type."
)

// Success Messages
const (
	SuccessOperationComplete = "✅ Operation completed successfully!"
	SuccessFileAdded         = "✅ File added successfully!"
	SuccessFileRemoved       = "✅ File removed successfully!"
	SuccessPaymentComplete   = "✅ Payment successful!"
	SuccessUsageReset        = "✅ Usage reset complete!"
	SuccessSaved             = "✅ Saved"
	SuccessCompleted         = "✅ Completed!"
	SuccessCancelled         = "❌ Cancelled"
)

// Progress Messages
const (
	ProgressStarting           = "🔄 Starting process..."
	ProgressProcessingTodo     = "🔄 Processing TODO..."
	ProgressLLMProcessing      = "🧠 LLM processing..."
	ProgressSavingToGitHub     = "📝 Saving to GitHub..."
	ProgressCheckingRepo       = "📊 Checking repository..."
	ProgressCheckingCapacity   = "📊 Checking repository capacity..."
	ProgressCheckingRemoteSize = "📊 Checking remote repository size..."
	ProgressProcessingPhoto    = "📷 Processing photo..."
	ProgressDownloadingPhoto   = "⬇️ Downloading photo..."
	ProgressUploadingPhoto     = "📝 Uploading photo to GitHub CDN..."
	ProgressPreparingSelection = "📋 Preparing file selection..."
)

// Command Descriptions
const (
	CmdStart      = "/start - Show this welcome message"
	CmdHelp       = "/help - Show detailed help and commands"
	CmdRepo       = "/repo - View repository information and settings"
	CmdSync       = "/sync - Synchronize issue statuses"
	CmdTodo       = "/todo - Show latest TODO items"
	CmdIssue      = "/issue - Show latest open issues"
	CmdCustomFile = "/customfile - Manage custom files"
	CmdInsight    = "/insight - View usage statistics and insights"
	CmdStats      = "/stats - View global bot statistics"
	CmdResetUsage = "/resetusage - Reset usage counters (paid service)"
	CmdCoffee     = "/coffee - Support the project and unlock premium features"
)

// Upgrade Messages
const (
	UpgradePrompt             = "Use /coffee to upgrade to premium for higher limits!"
	UpgradeForMoreFeatures    = "Upgrade with /coffee for more features!"
	UpgradeForMoreCustomFiles = "Upgrade with /coffee for more custom files!"
	UpgradeForMoreImages      = "Use /coffee to upgrade your plan for higher limits!"
	UpgradeContactAdmin       = "Please contact the administrator to upgrade."
)

// File Type Descriptions
const (
	DescNote   = "General notes and thoughts"
	DescTodo   = "Task items with checkboxes"
	DescIssue  = "Creates GitHub issues automatically"
	DescIdea   = "Ideas and brainstorming"
	DescInbox  = "Temporary storage"
	DescTool   = "Tool-related notes"
	DescCustom = "Your custom file paths"
)

// HTML Parse Mode
const (
	ParseModeHTML     = "HTML"
	ParseModeMarkdown = "MarkdownV2"
)

// Status Emojis
const (
	EmojiSuccess  = "✅"
	EmojiError    = "❌"
	EmojiWarning  = "⚠️"
	EmojiInfo     = "ℹ️"
	EmojiProgress = "🔄"
	EmojiFile     = "📁"
	EmojiPhoto    = "📷"
	EmojiIssue    = "❓"
	EmojiNote     = "📝"
	EmojiTodo     = "✅"
	EmojiIdea     = "💡"
	EmojiInbox    = "📥"
	EmojiTool     = "🔧"
	EmojiCustom   = "📁"
	EmojiCancel   = "❌"
	EmojiCoffee   = "☕"
	EmojiCake     = "🍰"
	EmojiSponsor  = "🎁"
	EmojiPremium  = "✨"
	EmojiChart    = "📊"
	EmojiInsight  = "📈"
	EmojiReset    = "🔄"
)

// Repository Status
const (
	StatusGreen  = "🟢"
	StatusYellow = "🟡"
	StatusRed    = "🔴"

	StatusPlentyOfSpace    = "Plenty of space available"
	StatusModerateUsage    = "Moderate usage"
	StatusHighUsage        = "High usage - consider cleanup"
	StatusAlmostFull       = "Almost full - cleanup needed soon"
	StatusRepositoryFull   = "Repository almost full"
	StatusCannotAddContent = "Cannot add more content when repository is nearly full"
)

// Custom File Messages
const (
	NoCustomFilesConfigured = "No custom files configured yet."
	CustomFileAdded         = "Custom file added successfully!"
	CustomFileRemoved       = "Custom file removed successfully!"
	CustomFileLimit         = "Custom file limit reached!"
	ChooseFileToRemove      = "Choose a file to remove:"
	ChooseFileToSave        = "Choose a file to save your message:"
)

// GitHub Related
const (
	GitHubLinkText     = "🔗 View on GitHub"
	GitHubSetupPrompt  = "Please configure your GitHub settings with /repo command"
	GitHubAuthFailed   = "GitHub authorization failed - please check your token and repository permissions. Use /repo to update your GitHub token"
	GitHubRepoNotFound = "Repository not found"
)

// Database Services
const (
	ServiceCoffee  = "COFFEE"
	ServiceCake    = "CAKE"
	ServiceSponsor = "SPONSOR"
	ServiceReset   = "RESET"
	ServiceRefund  = "REFUND"
)

// Subscription Change Log Operations
const (
	SubscriptionOperationTerminate = "TERMINATE"
	SubscriptionOperationReplace   = "REPLACE"
)

// Demo/Development
const (
	DemoWarning = "⚠️ This is a demo version. In production, this would redirect to Stripe or another payment processor."
	DevNote     = "This is for development/testing purposes only."
)

// Pagination
const (
	ShowMoreItems = "Show more..."
	NoMoreItems   = "No more items"
	PageSize      = 5
)

// File Extensions
const (
	ExtMarkdown = ".md"
	ExtImage    = ".jpg"
	ExtPNG      = ".png"
)

// Time Formats
const (
	TimeFormatDisplay = "2006-01-02 15:04"
	TimeFormatFile    = "20060102_150405"
)

// Limits and Thresholds
const (
	MaxMessageLength    = 4096
	MaxButtonTextLength = 30
	MaxFileNameDisplay  = 20
	MaxCustomFiles      = 20
	ProgressBarLength   = 6
	CapacityThreshold   = 90.0 // Repository capacity warning threshold
	MaxAssetsPerRelease = 100  // Maximum assets per GitHub release

	// Issue Management Limits
	IssueArchiveFile = "issue_archived.md" // Archive file name
)

// Default Values
const (
	DefaultTitle    = "untitled"
	DefaultFileName = "photo.jpg"
	DefaultCommit   = "Add content via Telegram"
)

// Stripe Webhook Messages
const (
	StripeWebhookSignatureError = "Stripe webhook signature verification failed"
	StripeScheduleCancelError   = "Stripe subscription schedule cancellation error"
	StripeNilSubscriptionError  = "Stripe subscription schedule has nil subscription"
	StripeScheduleCancelledMsg  = "Scheduled plan change has been cancelled"
)

// Subscription Messages
const (
	SubscriptionActivatedMsg         = "🎉 Subscription Activated!"
	SubscriptionReplacedWarning      = "⚠️ <b>Important:</b> You had an active subscription that has been replaced. Please cancel your old subscription to avoid duplicate charges."
	SubscriptionTerminatedMsg        = "❌ Your subscription has been terminated."
	LegacySubscriptionRenewedMsg     = "🔄 Legacy Subscription Renewed"
	SubscriptionReplacedNotification = "🔄 <b>Subscription Replaced</b>\n\n⚠️ Your current subscription has been replaced with a new one.\n\nPlease check whether your old subscription (ID: <code>%s</code>) is already scheduled to be cancelled to avoid duplicate charges.\n\n<i>You can manage your subscriptions through the Stripe Customer Portal in your email receipts.</i>"
)

// Benefits
const (
	MultipleLimit = "🚀 %dx repo size, photo and issue limit"
)
