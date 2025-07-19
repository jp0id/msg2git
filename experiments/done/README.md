# Commit Activity Graph Tool

A simple tool to visualize repository commit activity over the last 30 days, similar to GitHub's contribution graph.

## Usage

```bash
go run commit_graph.go <github-repo-url>
```

## Examples

```bash
# Using full GitHub URL
go run commit_graph.go https://github.com/golang/go

# Using owner/repo format
go run commit_graph.go golang/go

# Using SSH URL
go run commit_graph.go git@github.com:golang/go.git
```

## Features

- 📊 **Visual Graph**: Displays commits in a 3-row grid (10 days per row)
- 🎨 **Color Coding**: Uses emojis to represent different activity levels
- 📈 **Statistics**: Shows total commits and max commits per day
- 📅 **Recent Activity**: Lists recent commit activity
- 🔐 **GitHub Token Support**: Uses `GITHUB_TOKEN` environment variable if available

## Authentication (Optional)

For higher rate limits, set your GitHub personal access token:

```bash
export GITHUB_TOKEN=your_github_token_here
go run commit_graph.go golang/go
```

## Output Format

The tool displays:
1. **Dates**: MM/DD format for each day
2. **Activity Blocks**: Colored emoji squares representing commit levels
3. **Commit Counts**: Number of commits per day (or "-" for zero)

### Activity Levels

- ⬜ 0 commits
- 🟩 1-2 commits  
- 🟨 3-5 commits
- 🟧 6-10 commits
- 🟥 11+ commits

## Sample Output

```
📊 Fetching commit activity for golang/go (last 30 days)

Total commits: 74
Max commits per day: 8

    06/01 06/02 06/03 06/04 06/05 06/06 06/07 06/08 06/09 06/10
    ⬜    🟨    🟧    🟩    🟨    🟩    ⬜    🟩    🟨    🟧    
    -     3     8     2     5     1     -     1     5     7    

    06/11 06/12 06/13 06/14 06/15 06/16 06/17 06/18 06/19 06/20
    🟨    🟩    🟨    🟨    ⬜    🟨    🟩    🟩    🟩    🟩    
    3     2     4     3     -     3     1     1     1     2    

    06/21 06/22 06/23 06/24 06/25 06/26 06/27 06/28 06/29 06/30
    🟩    🟩    🟨    🟨    🟨    🟩    🟧    ⬜    ⬜    ⬜    
    1     2     4     3     4     2     6     -     -     -    

Recent Activity:
  Fri Jun 27: 6 commit(s)
  Thu Jun 26: 2 commit(s)
  Wed Jun 25: 4 commit(s)
  Tue Jun 24: 3 commit(s)
```

## Dependencies

- Go 1.16+ (uses built-in packages only)
- Internet connection to access GitHub API

## Limitations

- Only shows the last 30 days of commit activity
- Limited to 100 commits per API call (GitHub API limitation)
- Public repositories only (unless GitHub token is provided)
- Rate limited by GitHub API (60 requests/hour without token, 5000 with token)