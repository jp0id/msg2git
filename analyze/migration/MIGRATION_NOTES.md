# Database Migration Notes

## Committer Field Migration

**Date**: 2025-07-12
**Description**: Moved committer field from `premium_user` table to `users` table to make the feature available to all users.

### Changes Made:

1. **Database Schema Changes Required:**
   ```sql
   -- Add committer column to users table (REQUIRED)
   ALTER TABLE users ADD COLUMN IF NOT EXISTS committer TEXT DEFAULT '';
   
   -- Migrate existing committer data from premium_user to users (if exists)
   UPDATE users 
   SET committer = pu.committer 
   FROM premium_user pu 
   WHERE users.chat_id = pu.uid 
   AND pu.committer IS NOT NULL 
   AND pu.committer != '';
   
   -- Remove committer column from premium_user table (if exists)
   ALTER TABLE premium_user DROP COLUMN IF EXISTS committer;
   ```
   
   **⚠️ IMPORTANT**: The `committer` column MUST be added to the `users` table before deploying the new code, otherwise the application will fail with database errors.

2. **Code Changes:**
   - Added `Committer` field to `User` model
   - Removed `Committer` field from `PremiumUser` model
   - Updated `getCommitterInfo()` to read from `users` table
   - Removed `UpdatePremiumUserCommitter()` function
   - Updated all premium_user database queries to exclude committer column
   - Fixed `GetUserByChatID()` to select and scan committer column
   - Fixed user creation queries to include committer in RETURNING clause

3. **Feature Changes:**
   - Committer feature is now available to all users (removed premium requirement)
   - `/repo` command now reads committer from `users` table
   - `/committer` command uses `UpdateUserCommitter()` function

4. **Enhanced `/repo` Command Features:**
   - Added size source information: "(Actual cloned size)" or "(Remote API)"
   - Added GitHub token status section showing if token is configured
   - Improved display format with comprehensive repository information
   - Better organization of information sections

### Deployment Notes:
- Run the SQL migration before deploying the new code
- The migration is backwards compatible - existing committer data will be preserved
- Users who previously set committers will retain their settings
- Enhanced `/repo` command provides better visibility into repository configuration