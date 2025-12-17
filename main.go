package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type JiraConfig struct {
	Email string
	URL   string
	Token string
}

type Sprint struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type IssueFields struct {
	Summary   string `json:"summary"`
	IssueType struct {
		Name string `json:"name"`
	} `json:"issuetype"`
	Status struct {
		Name string `json:"name"`
	} `json:"status"`
	Points  float64  `json:"customfield_10004"`
	Sprints []Sprint `json:"customfield_10007"`
}

type JiraIssue struct {
	Key    string      `json:"key"`
	Fields IssueFields `json:"fields"`
}

type Transition struct {
	ID string `json:"id"`
	To struct {
		Name string `json:"name"`
	} `json:"to"`
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing env: %s", key)
	}
	return v
}

func authHeader(cfg JiraConfig) string {
	raw := cfg.Email + ":" + cfg.Token
	token := base64.StdEncoding.EncodeToString([]byte(raw))
	return "Basic " + token
}

func doJSON(cfg JiraConfig, method, url string, body any, out any) error {
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(buf)
	}

	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", authHeader(cfg))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("jira error: %d %s", res.StatusCode, res.Status)
	}

	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}

	return nil
}

func getIssues(cfg JiraConfig) ([]JiraIssue, error) {
	var out struct {
		Issues []JiraIssue `json:"issues"`
	}

	body := map[string]any{
		"jql":    "assignee = currentUser() AND statusCategory != Done AND issuetype != Epic",
		"fields": []string{"summary", "customfield_10004", "issuetype", "status", "customfield_10007"},
	}

	err := doJSON(cfg, http.MethodPost, cfg.URL+"/rest/api/3/search/jql", body, &out)
	return out.Issues, err
}

func getTransitions(cfg JiraConfig, issueKey string) ([]Transition, error) {
	var out struct {
		Transitions []Transition `json:"transitions"`
	}

	url := fmt.Sprintf("%s/rest/api/3/issue/%s/transitions", cfg.URL, issueKey)
	err := doJSON(cfg, http.MethodGet, url, nil, &out)
	return out.Transitions, err
}

func transitionIssue(cfg JiraConfig, issueKey, targetStatus string) error {
	transitions, err := getTransitions(cfg, issueKey)
	if err != nil {
		return err
	}

	var match *Transition
	for i := range transitions {
		if strings.EqualFold(transitions[i].To.Name, targetStatus) {
			match = &transitions[i]
			break
		}
	}
	if match == nil {
		names := make([]string, len(transitions))
		for i, t := range transitions {
			names[i] = t.To.Name
		}
		return fmt.Errorf("no transition to %q for issue %s (available: %s)", targetStatus, issueKey, strings.Join(names, ", "))
	}

	body := map[string]any{
		"transition": map[string]any{"id": match.ID},
	}

	url := fmt.Sprintf("%s/rest/api/3/issue/%s/transitions", cfg.URL, issueKey)
	return doJSON(cfg, http.MethodPost, url, body, nil)
}

func addIssueToSprint(cfg JiraConfig, sprintID int, issueKey string) error {
	body := map[string]any{
		"issues": []string{issueKey},
	}
	url := fmt.Sprintf("%s/rest/agile/1.0/sprint/%d/issue", cfg.URL, sprintID)
	return doJSON(cfg, http.MethodPost, url, body, nil)
}

func findActiveSprint(issues []JiraIssue) (*Sprint, error) {
	for _, ji := range issues {
		for i := range ji.Fields.Sprints {
			sp := ji.Fields.Sprints[i]
			if strings.EqualFold(sp.State, "active") {
				s := sp
				return &s, nil
			}
		}
	}
	return nil, fmt.Errorf("no active sprint found in current issues")
}

func moveIssueToCurrentSprint(cfg JiraConfig, issueKey string) error {
	issues, err := getIssues(cfg)
	if err != nil {
		return err
	}

	s, err := findActiveSprint(issues)
	if err != nil {
		return err
	}

	return addIssueToSprint(cfg, s.ID, issueKey)
}

func sprintName(s []Sprint) string {
	if len(s) == 0 {
		return "Backlog"
	}
	for _, sp := range s {
		if sp.State == "active" {
			return sp.Name
		}
	}
	return s[len(s)-1].Name
}

func formatPoints(p float64) string {
	if p == 0 {
		return "-"
	}
	return strconv.Itoa(int(p + 0.5))
}

func formatIssuesBySprint(issues []JiraIssue) string {
	groups := map[string][]JiraIssue{}

	for _, ji := range issues {
		n := sprintName(ji.Fields.Sprints)
		groups[n] = append(groups[n], ji)
	}

	var lines []string
	for sprint, list := range groups {
		var total float64
		for _, ji := range list {
			total += ji.Fields.Points
		}
		lines = append(lines, fmt.Sprintf(
			"Sprint: %s (%d issues, %s pts)",
			sprint, len(list), formatPoints(total),
		))

		for _, ji := range list {
			f := ji.Fields
			lines = append(lines, fmt.Sprintf(
				"  %s\t%s\t%s\t%s\t%s",
				ji.Key,
				formatPoints(f.Points),
				f.Status.Name,
				f.IssueType.Name,
				f.Summary,
			))
		}
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

func issueLabel(i JiraIssue) string {
	if i.Fields.Status.Name == "" {
		return i.Key + "  " + i.Fields.Summary
	}
	return i.Key + "  " + i.Fields.Summary + "  [" + i.Fields.Status.Name + "]"
}

func pickFromList(label string, items []string) int {
	r := bufio.NewReader(os.Stdin)
	for i, item := range items {
		fmt.Printf("%d) %s\n", i+1, item)
	}
	fmt.Printf("%s (1-%d, empty to cancel): ", label, len(items))

	line, err := r.ReadString('\n')
	if err != nil {
		log.Fatalf("read error: %v", err)
	}

	trim := strings.TrimSpace(line)
	if trim == "" {
		return -1
	}

	n, err := strconv.Atoi(trim)
	if err != nil || n < 1 || n > len(items) {
		log.Fatalf("invalid selection")
	}
	return n - 1
}

func selectIssue(cfg JiraConfig, filter func(JiraIssue) bool, prompt string) (*JiraIssue, error) {
	issues, err := getIssues(cfg)
	if err != nil {
		return nil, err
	}

	fmt.Println(formatIssuesBySprint(issues))

	var list []JiraIssue
	for _, ji := range issues {
		if filter == nil || filter(ji) {
			list = append(list, ji)
		}
	}

	if len(list) == 0 {
		return nil, nil
	}

	labels := make([]string, len(list))
	for i, is := range list {
		labels[i] = issueLabel(is)
	}

	idx := pickFromList(prompt, labels)
	if idx == -1 {
		return nil, nil
	}

	return &list[idx], nil
}

func interactiveFlow(cfg JiraConfig) error {
	issue, err := selectIssue(cfg, nil, "Select issue")
	if err != nil {
		return err
	}
	if issue == nil {
		return nil
	}

	statuses := []string{"Open", "In Progress", "In Review", "In Testing", "Resolved"}
	si := pickFromList("Select new status", statuses)
	if si == -1 {
		return nil
	}

	if err := transitionIssue(cfg, issue.Key, statuses[si]); err != nil {
		return err
	}

	fmt.Printf("Transitioned %s to %q\n", issue.Key, statuses[si])

	if len(issue.Fields.Sprints) == 0 {
		if err := moveIssueToCurrentSprint(cfg, issue.Key); err != nil {
			return err
		}
		fmt.Printf("Added %s to active sprint\n", issue.Key)
	}

	return nil
}

func moveFlow(cfg JiraConfig, issueKey string) error {
	if issueKey == "" {
		issue, err := selectIssue(cfg, func(j JiraIssue) bool {
			return len(j.Fields.Sprints) == 0
		}, "Select issue to move")
		if err != nil {
			return err
		}
		if issue == nil {
			return nil
		}
		issueKey = issue.Key
	}

	if err := moveIssueToCurrentSprint(cfg, issueKey); err != nil {
		return err
	}
	fmt.Printf("Added %s to active sprint\n", strings.ToUpper(issueKey))
	return nil
}

func main() {
	cfg := JiraConfig{
		Email: mustEnv("JIRA_EMAIL"),
		URL:   strings.TrimRight(mustEnv("JIRA_URL"), "/"),
		Token: mustEnv("JIRA_API_TOKEN"),
	}

	args := os.Args[1:]

	if len(args) == 0 {
		issues, err := getIssues(cfg)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(formatIssuesBySprint(issues))
		return
	}

	if args[0] == "-i" {
		if err := interactiveFlow(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	if args[0] == "-m" {
		var key string
		if len(args) > 1 {
			key = args[1]
		}
		if err := moveFlow(cfg, key); err != nil {
			log.Fatal(err)
		}
		return
	}

	issueKey := args[0]
	status := strings.TrimSpace(strings.Join(args[1:], " "))
	if status == "" {
		log.Fatalf("missing target status")
	}
	if err := transitionIssue(cfg, issueKey, status); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Transitioned %s to %q\n", issueKey, status)
}
