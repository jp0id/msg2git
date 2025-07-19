# premium features:

## v1 features:
- [ ] premium_user table (after give me first /coffee)
  id: increment_id
  uid: user id
  username: user name
  committer: default ''
  level: premium_level (1 means lowest)
  created_at: timestamp
  expire_at: timestamp of premium expiry, -1 will not expiry
- [ ] user_topup_log table (use to record premium user)
  id: increment_id
  uid: chat id (need index)
  username: user name
  amount: paid amount
  created_at: date
- [ ] /coffee command maybe stripe or something? return a menu for user to choose, can send a invoice or something. Each user maximum upgrade each repo size once (future maybe allow upgrade more times)
- [ ] improve /reposize to dynamic read max reposize * premium level but not expired, if no premium record, use default
- [ ] /committer custom set committer after give me at least one coffee

## v2 features:
- [ ] 📁 Custom File ( level 0 max 2 files, level 1 max 4 files, level 2 max 8 files, level 3 max 12 files ), add a [📁 Custom]  button upon user send a message, beside NOTE, INBOX, etc...
   feature 1. if any file already added, will shown in message when click [📁 Custom] button, choose the file from message to commit, interaction can be copy file name path, reply to the message
   feature 2. if user want add files, reply to choose location with file name path ( if file exists then just update a db record, else create file and necessary directories)
   feature 3. need store this info to db, add a column to user table, store the custom file set
   feature 4. can minus a file, sync with db also
   feature 5. upgrade hint to scale custom file list upon reaching limit
- [ ] issue create limitation each free tier user can create 100 issues, premium with 2x, 4x, 10x  need user_insights table to record the count, the table schema:
  id: increment
  uid: user id (index needed)
  commit_cnt: count of total messages stored to github
  issue_cnt: count of created issues
  image_cnt: count of uploaded images ()
  repo_size: current repo actual size in MB
  update_time: any update will update this column
- [ ] /coffee, /cake, /sponsor all premium set to 1 year expiry

## v3 features:
- [ ] reset usage feature
  User can use /resetusage command to clear the count of issue create and photo upload.
  Reset usage service must need user pay to use, can use same interaction flow as premium purchase
  Need update database table:
    1. create user_usage table (a bit like user_insights table, but the data can be changed, to record user usage info)
      id: increment
      uid: user id (index needed)
      issue_cnt: count of created issues
      image_cnt: count of uploaded images
      update_time: any update will update this column
    2. add column for user_insights table
      reset_cnt: count of usage reset
    3. add column for user_topup_log table
      service: [COFFEE|CAKE|SPONSOR|RESET] to record user topup purpose
- [ ] /insight command to show:
  1. usage info
  2. insights info
  3. premium info
  4. other good insights info
  every metric can give a friendly graph like percentage bar.
  this command is available for all tier users.

## v4 features:
- [ ] subscriptino_change_log table (use to record subscription replaced and canceled events)
  id: increment_id
  uid: chat id (need index)
  subscription_id: subscription id of stripe (need index)
  operation: [TERMINATE|REPLACE] to record subscription change reason, REPLACE only happens when user duplicated create subscription, TERMINATE happens when user do not renew, or canceled by admin.
  created_at: date (need index)
- [ ] when received subscription terminate webhook will add a record to this table
- [ ] when received subscription creation, will query premium_user and if found user already have a subscription_id with expire_at after now, means still active, will add a record to this table with old subscription_id replaced.
- [ ] when active subscription_id replaced, system will send a message start with '🎉 Subscription Activated!...', can append a warning hint inside this message, to reminder user cancel old active subscriptions.

## v5 feautres:
- [ ] default llm processor
  .env.example have configured llm configs:
  LLM_PROVIDER=Deepseek
  LLM_ENDPOINT=https://api.deepseek.com/v1
  LLM_MODEL=deepseek-chat
  LLM_TOKEN=sk-xxxx

  before llm process, if llm switch is on, but user haven't settle llm token, then:
  1. check user free token usage and its limit (free tier 100000 tokens for total, coffee 2x, cake 4x, sponsor 10x, this implementation can refer to existing logics like getRepositoryMultiplier)
  2. if usage+current message's length less than limit, then use default llm processor 
  3. if exceed limit, then simply skip llm process

