package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/nlopes/slack"
	"github.com/pkg/errors"
)

var (
	token       string
	slackClient *slack.Client

	whitelistedEmails = []string{
		"stephen.meriwether@policygenius.com",
		"adam.chadroff@policygenius.com",
		"jason.fromm@policygenius.com",
	}
)

const (
	teamID    = 5285                 // "Product" team
	nHoursAgo = 18                   // Number of hours a story needs to be in acceptance to care
	username  = "Clubhouse Notifier" // Slack bot username
)

func init() {
	token = os.Getenv("CLUBHOUSE_API_TOKEN")
	slackClient = slack.New(os.Getenv("SLACK_API_TOKEN"))
}

func main() {
	lambda.Start(run)
}

func run() {
	slackUsers, err := fetchSlackUsers()
	if err != nil {
		panic(err)
	}

	fmt.Println("Found N slack users", len(slackUsers))

	whitlistedSlackUsers := slackUsersForEmails(slackUsers, whitelistedEmails)

	fmt.Println("Found N whitelisted slack users", len(whitlistedSlackUsers))

	clubhouseUsers, err := fetchClubhouseUsers()
	if err != nil {
		panic(err)
	}

	fmt.Println("Found N clubhouse users", len(clubhouseUsers))

	whitelistedClubhouseUsers := clubhouseUsersForEmails(clubhouseUsers, whitelistedEmails)

	fmt.Println("Found N whitelisted clubhouse users", len(whitelistedClubhouseUsers))

	workflows, err := fetchWorkflows()
	if err != nil {
		panic(err)
	}

	acceptanceWorkflowState := workflowStateForTeamWithName(workflows, teamID, "In Acceptance")
	if acceptanceWorkflowState == nil {
		panic("Nil acceptanceWorkflowState")
	}

	projects, err := fetchProjects()
	if err != nil {
		panic(err)
	}

	projectsForTeam := projectsForTeam(projects, teamID)

	fmt.Println("Found N projects for team", len(projectsForTeam))

	var stories []Story
	for _, project := range projectsForTeam {
		fetchedStories, err := fetchStories(project.ID)
		if err != nil {
			panic(err)
		}

		stories = append(stories, fetchedStories...)
	}

	fmt.Println("Found N stories", len(stories))

	storiesInAcceptance := storiesInWorkflowState(stories, *acceptanceWorkflowState)

	fmt.Println("Found N stories in acceptance", len(storiesInAcceptance))

	storiesForWhitelistedUsers := storiesForRequesters(storiesInAcceptance, whitelistedClubhouseUsers)

	fmt.Println("Found N stories in acceptance for whitelisted users", len(storiesForWhitelistedUsers))

	oldStoriesInAcceptance := storiesOlderThanNHoursAgo(storiesForWhitelistedUsers, nHoursAgo)

	fmt.Println("Found N old stories in acceptance", len(oldStoriesInAcceptance))

	err = notifySlackOfStaleStories(oldStoriesInAcceptance, whitelistedClubhouseUsers, whitlistedSlackUsers)
	if err != nil {
		panic(err)
	}
}

func fetchSlackUsers() ([]slack.User, error) {
	return slackClient.GetUsers()
}

func fetchClubhouseUsers() ([]User, error) {
	resp, err := makeRequest("members")
	if err != nil {
		return []User{}, errors.Wrap(err, "Members request failed")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []User{}, errors.Wrap(err, "Failed to read response body")
	}
	defer resp.Body.Close()

	var users []User
	if err := json.Unmarshal(body, &users); err != nil {
		return []User{}, errors.Wrap(err, "Failed to unmarshal response body into user object")
	}

	return users, nil
}

func fetchProjects() ([]Project, error) {
	resp, err := makeRequest("projects")
	if err != nil {
		return []Project{}, errors.Wrap(err, "Project request failed")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []Project{}, errors.Wrap(err, "Failed to read response body")
	}
	defer resp.Body.Close()

	var projects []Project
	if err := json.Unmarshal(body, &projects); err != nil {
		return []Project{}, errors.Wrap(err, "Failed to unmarshal response body into project object")
	}

	return projects, nil
}

func fetchStories(projectID int) ([]Story, error) {
	resp, err := makeRequest(fmt.Sprintf("projects/%d/stories", projectID))
	if err != nil {
		return []Story{}, errors.Wrap(err, "Stories request failed")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []Story{}, errors.Wrap(err, "Failed to read response body")
	}
	defer resp.Body.Close()

	var stories []Story
	if err := json.Unmarshal(body, &stories); err != nil {
		return []Story{}, errors.Wrap(err, "Failed to unmarshal response body into story object")
	}

	return stories, nil
}

func fetchWorkflows() ([]Workflow, error) {
	resp, err := makeRequest("workflows")
	if err != nil {
		return []Workflow{}, errors.Wrap(err, "Workflow request failed")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []Workflow{}, errors.Wrap(err, "Failed to read response body")
	}
	defer resp.Body.Close()

	var workflows []Workflow
	if err := json.Unmarshal(body, &workflows); err != nil {
		return []Workflow{}, errors.Wrap(err, "Failed to unmarshal response body into workflow object")
	}

	return workflows, nil
}

func slackUsersForEmails(users []slack.User, whitelistedEmails []string) []slack.User {
	var whitelistedUsers []slack.User
	for _, user := range users {
		for _, email := range whitelistedEmails {
			if user.Profile.Email == email {
				whitelistedUsers = append(whitelistedUsers, user)
				break
			}
		}
	}

	return whitelistedUsers
}

func clubhouseUsersForEmails(users []User, whitelistedEmails []string) []User {
	var whitelistedUsers []User
	for _, user := range users {
		for _, email := range whitelistedEmails {
			if user.Profile.Email == email {
				whitelistedUsers = append(whitelistedUsers, user)
				break
			}
		}
	}

	return whitelistedUsers
}

func workflowStateForTeamWithName(workflows []Workflow, teamID int, workflowStateName string) *WorkflowState {
	for _, workflow := range workflows {
		if workflow.TeamID != teamID {
			continue
		}

		for _, state := range workflow.States {
			if state.Name == workflowStateName {
				return &state
			}
		}
	}

	return nil
}

func projectsForTeam(projects []Project, teamID int) []Project {
	var projectsForTeam []Project
	for _, project := range projects {
		if project.TeamID != teamID {
			continue
		}

		projectsForTeam = append(projectsForTeam, project)
	}

	return projectsForTeam
}

func storiesInWorkflowState(stories []Story, workflowState WorkflowState) []Story {
	var storiesInWorkflowState []Story
	for _, story := range stories {
		if story.WorkflowStateID != workflowState.ID {
			continue
		}

		storiesInWorkflowState = append(storiesInWorkflowState, story)
	}

	return storiesInWorkflowState
}

func storiesForRequesters(stories []Story, whitelistedUsers []User) []Story {
	var whitelistedStories []Story
	for _, story := range stories {
		for _, user := range whitelistedUsers {
			if user.ID == story.RequesterID {
				whitelistedStories = append(whitelistedStories, story)
				break
			}
		}
	}

	return whitelistedStories
}

func storiesOlderThanNHoursAgo(storiesInAcceptance []Story, nHoursAgo int) []Story {
	var oldStories []Story
	for _, story := range storiesInAcceptance {
		if story.MovedInLastNHours(nHoursAgo) {
			oldStories = append(oldStories, story)
		}
	}

	return oldStories
}

func makeRequest(resourcePath string) (*http.Response, error) {
	url := fmt.Sprintf(
		"https://api.clubhouse.io/api/v2/%s?token=%s",
		resourcePath, token,
	)

	return http.Get(url)
}

func notifySlackOfStaleStories(stories []Story, clubhouseUsers []User, slackUsers []slack.User) error {
	var errors []error
	for _, slackUser := range slackUsers {
		var clubhouseUser User
		for _, user := range clubhouseUsers {
			if user.Profile.Email == slackUser.Profile.Email {
				clubhouseUser = user
				break
			}
		}

		var storiesForSlackUser []Story
		for _, story := range stories {
			if story.RequesterID == clubhouseUser.ID {
				storiesForSlackUser = append(storiesForSlackUser, story)
			}
		}

		if len(storiesForSlackUser) == 0 {
			continue
		}

		message := fmt.Sprintf(
			"You have %d story(s) in acceptance for more than %d hours\n",
			len(storiesForSlackUser), nHoursAgo,
		)

		for _, story := range storiesForSlackUser {
			message += fmt.Sprintf(
				"* <%s|%s> was moved %v ago\n", story.url(), story.Name, story.ago(),
			)
		}

		_, _, err := slackClient.PostMessage(
			slackUser.ID,
			message,
			slack.PostMessageParameters{
				Username: username,
				AsUser:   true,
			},
		)
		if err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) == 0 {
		return nil
	}

	var errorMsg string
	for _, err := range errors {
		errorMsg += err.Error()
	}

	return fmt.Errorf("%s", errorMsg)
}

type Project struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	TeamID int    `json:"team_id"`
}

type Story struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	RequesterID     string `json:"requested_by_id"`
	WorkflowStateID int    `json:"workflow_state_id"`
	MovedAt         string `json:"moved_at"`
}

func (s Story) url() string {
	return fmt.Sprintf("https://app.clubhouse.io/policygenius/story/%d", s.ID)
}

func (s Story) MovedInLastNHours(n int) bool {
	parsedMovedAt, err := time.Parse(time.RFC3339, s.MovedAt)
	if err != nil {
		return false
	}

	nHoursAgo := time.Now().Add(time.Duration(-n) * time.Hour)
	return parsedMovedAt.Before(nHoursAgo)
}

func (s Story) ago() time.Duration {
	parsedMovedAt, err := time.Parse(time.RFC3339, s.MovedAt)
	if err != nil {
		return -1
	}

	return time.Now().Sub(parsedMovedAt)
}

type Workflow struct {
	ID     int             `json:"id"`
	States []WorkflowState `json:"states"`
	TeamID int             `json:"team_id"`
}

type WorkflowState struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type User struct {
	ID      string      `json:"id"`
	Profile UserProfile `json:"profile"`
}

type UserProfile struct {
	Email string `json:"email_address"`
}
