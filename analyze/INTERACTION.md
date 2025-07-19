# INTERACTION

## Save a note

User: There's a meeting tomorrow
Bot: Please choose a location:
     [📝 NOTE] [✅ TODO] [❓ ISSUE] [💡 IDEA] [📥 INBOX] [🔧 TOOL] [❌ CANCEL]

After user clicks the button:
Bot: ✅ Saved to TODO

> here content will be saved to the TODO.md file in the repo, same as NOTE.md, IDEAS.md ...
> only if the message don't have \n then can save to TODO.md

## Reply to edit a message

User replies to a message: /edit Meeting at 3 PM tomorrow  
Bot: ✅ Content updated  

User replies to a message: /done  
Bot: ✅ Marked as completed ~~Meeting tomorrow~~ ✓


