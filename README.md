# jira-cli

A minimal, fast Jira command-line tool.  
Lists your assigned issues, shows sprint summaries, transitions issues, and moves backlog items into the active sprint.

## Features

- Show all issues assigned to you  
- Sprint summaries with story-point totals  
- Transition issues to another workflow state  
- Move backlog issues into the currently active sprint  
- Optional interactive mode

## Install

```
go install github.com/jsubroto/jira-cli@latest
```

## Configuration

The CLI reads **environment variables only**.  
If you keep them in a `.env` file, load them using your preferred method (`dotenv`, `direnv`, manual export). The binary does not read `.env` itself.

Required variables:

```
JIRA_EMAIL=you@example.com
JIRA_API_TOKEN=your_api_token
JIRA_URL=https://yourcompany.atlassian.net
```

No board ID is required. The tool infers the active sprint from your assigned issues.

## Usage

### List your issues
```
jira-cli
```

### Transition an issue
```
jira-cli ABC-123 "In Progress"
```
The status text must match one of the available transitions for that issue.

### Move an issue into the active sprint
```
jira-cli -m ABC-123
```
If no key is provided, you’ll be prompted to pick an unsprinted issue.

### Interactive mode
```
jira-cli -i
```
Pick an issue and a new status; issues not in a sprint are auto-added to the active sprint.

## License

MIT
