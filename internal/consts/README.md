# Constants Package

This package contains all frequently used constants throughout the msg2git application. By centralizing constants, we improve maintainability, consistency, and reduce the risk of typos.

## Organization

Constants are organized into logical groups:

### Core Application
- **File Types**: NOTE, TODO, ISSUE, etc.
- **Button Labels**: UI button text with emojis
- **Premium Tiers**: Coffee, Cake, Sponsor information

### Messages
- **Error Messages**: Common error responses
- **Success Messages**: Operation confirmations
- **Progress Messages**: Status updates during operations
- **Upgrade Messages**: Premium upgrade prompts

### UI Elements
- **Status Emojis**: Visual indicators
- **Repository Status**: Capacity and usage messages
- **GitHub Related**: Links and setup messages

### Technical
- **Database Services**: Payment service types
- **Limits and Thresholds**: Application limits
- **Time Formats**: Date/time formatting
- **File Extensions**: Supported file types

## Usage Examples

```go
import "github.com/msg2git/msg2git/internal/consts"

// Using file type constants
filename := consts.FileNameTodo // "todo.md"
fileType := consts.FileTypeTodo // "TODO"

// Using button labels
button := tgbotapi.NewInlineKeyboardButtonData(consts.ButtonNote, callbackData)

// Using error messages
if err != nil {
    bot.SendResponse(chatID, consts.ErrorGitHubNotConfigured)
}

// Using premium tier information
price := consts.PriceCoffee // 5.0
tier := consts.TierCoffee   // "☕ Coffee"

// Using progress messages
bot.UpdateProgressMessage(chatID, messageID, 50, consts.ProgressLLMProcessing)
```

## Guidelines for Adding Constants

### When to Add a Constant
- String appears in multiple files
- String is central to application functionality
- String contains important UI text or messages
- String represents a configuration value

### Naming Conventions
- Use descriptive, self-documenting names
- Group related constants with common prefixes:
  - `Button*` for UI button labels
  - `Error*` for error messages
  - `Success*` for success messages
  - `Progress*` for progress updates
  - `FileType*` / `FileName*` for file-related constants

### Organization
- Add new constants to appropriate existing groups
- Create new groups for new categories of constants
- Keep related constants together
- Use clear comments for group sections

## Benefits

1. **Consistency**: All UI text and messages are consistent across the application
2. **Maintainability**: String changes only need to be made in one place
3. **Type Safety**: Reduces typos in string literals
4. **Self-Documentation**: Constant names clearly indicate their purpose
5. **Internationalization Ready**: Easy foundation for future i18n support
6. **Testing**: Constants can be easily mocked or tested

## Migration Strategy

When refactoring existing code to use constants:

1. Identify frequently used strings
2. Add appropriate constants to this package
3. Replace hardcoded strings with constant references
4. Test to ensure functionality is preserved
5. Update documentation as needed

## Future Considerations

- **Internationalization**: This structure supports future i18n by replacing constants with localized strings
- **Configuration**: Some constants may become configurable in the future
- **Themes**: UI strings could support theming or customization