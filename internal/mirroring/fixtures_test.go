package mirroring

import (
	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

var (

	// TEST_RELEASE is a test release used in unit tests.
	testReleaseString = `{
		"tag_name": "v0.1",
		"description": "description",
		"name": "Awesome app v0.1 alpha",
		"description_html": "description_html",
		"created_at": "2019-01-03T01:55:18.203Z",
		"author": {
			"id": 1,
			"name": "Administrator",
			"username": "root",
			"state": "active",
			"avatar_url": "https://www.gravatar.com/avatar/",
			"web_url": "http://localhost:3000/root"
		},
		"commit": {
			"id": "f8d3d94cbd347e924aa7b715845e439d00e80ca4",
			"short_id": "f8d3d94c",
			"title": "Initial commit",
			"created_at": "2019-01-03T01:53:28.000Z",
			"parent_ids": [],
			"message": "Initial commit",
			"author_name": "Administrator",
			"author_email": "admin@example.com",
			"authored_date": "2019-01-03T01:53:28.000Z",
			"committer_name": "Administrator",
			"committer_email": "admin@example.com",
			"committed_date": "2019-01-03T01:53:28.000Z"
		},
		"assets": {
			"count": 2,
			"sources": [
			{
				"format": "zip",
				"url": "http://localhost:3000/archive/v0.1/awesome-app-v0.1.zip"
			},
			{
				"format": "tar.gz",
				"url": "http://localhost:3000/archive/v0.1/awesome-app-v0.1.tar.gz"
			}
			],
			"links": []
		}
	}`

	// testReleasesString is a string representation of multiple test releases.
	testReleasesString = []string{
		`{
			"tag_name": "v0.2",
			"description": "description",
			"name": "Awesome app v0.2 beta",
			"description_html": "html",
			"created_at": "2019-01-03T01:56:19.539Z",
			"author": {
			"id": 1,
			"name": "Administrator",
			"username": "root",
			"state": "active",
			"avatar_url": "https://www.gravatar.com/avatar",
			"web_url": "http://localhost:3000/root"
			},
			"commit": {
			"id": "079e90101242458910cccd35eab0e211dfc359c0",
			"short_id": "079e9010",
			"title": "Update README.md",
			"created_at": "2019-01-03T01:55:38.000Z",
			"parent_ids": [
				"f8d3d94cbd347e924aa7b715845e439d00e80ca4"
			],
			"message": "Update README.md",
			"author_name": "Administrator",
			"author_email": "admin@example.com",
			"authored_date": "2019-01-03T01:55:38.000Z",
			"committer_name": "Administrator",
			"committer_email": "admin@example.com",
			"committed_date": "2019-01-03T01:55:38.000Z"
			},
			"assets": {
			"count": 4,
			"sources": [
				{
				"format": "zip",
				"url": "http://localhost:3000/archive/v0.2/awesome-app-v0.2.zip"
				},
				{
				"format": "tar.gz",
				"url": "http://localhost:3000/archive/v0.2/awesome-app-v0.2.tar.gz"
				}
			],
			"links": [
				{
				"id": 2,
				"name": "awesome-v0.2.msi",
				"url": "http://192.168.10.15:3000/msi",
				"external": true
				},
				{
				"id": 1,
				"name": "awesome-v0.2.dmg",
				"url": "http://192.168.10.15:3000",
				"external": true
				}
			]
			}
		}`,
		`{
			"tag_name": "v0.1",
			"description": "description",
			"name": "Awesome app v0.1 alpha",
			"description_html": "description_html",
			"created_at": "2019-01-03T01:55:18.203Z",
			"author": {
			"id": 1,
			"name": "Administrator",
			"username": "root",
			"state": "active",
			"avatar_url": "https://www.gravatar.com/avatar",
			"web_url": "http://localhost:3000/root"
			},
			"commit": {
			"id": "f8d3d94cbd347e924aa7b715845e439d00e80ca4",
			"short_id": "f8d3d94c",
			"title": "Initial commit",
			"created_at": "2019-01-03T01:53:28.000Z",
			"parent_ids": [],
			"message": "Initial commit",
			"author_name": "Administrator",
			"author_email": "admin@example.com",
			"authored_date": "2019-01-03T01:53:28.000Z",
			"committer_name": "Administrator",
			"committer_email": "admin@example.com",
			"committed_date": "2019-01-03T01:53:28.000Z"
			},
			"assets": {
			"count": 2,
			"sources": [
				{
				"format": "zip",
				"url": "http://localhost:3000/archive/v0.1/awesome-app-v0.1.zip"
				},
				{
				"format": "tar.gz",
				"url": "http://localhost:3000/archive/v0.1/awesome-app-v0.1.tar.gz"
				}
			],
			"links": []
			}
		}`,
	}

	issueString = "issue"

	testIssue = &gitlab.Issue{
		IID:          1,
		Title:        "Test Issue",
		Description:  "This is a test issue",
		Labels:       []string{"bug", "urgent"},
		Confidential: false,
		Weight:       3,
		IssueType:    &issueString,
	}

	// testIssueString is the string representation of testIssue.
	testIssueString = `{
      "project_id" : 1,
      "milestone" : {
         "due_date" : null,
         "project_id" : 4,
         "state" : "closed",
         "description" : "Rerum est voluptatem provident consequuntur molestias similique ipsum dolor.",
         "iid" : 3,
         "id" : 11,
         "title" : "v3.0",
         "created_at" : "2016-01-04T15:31:39.788Z",
         "updated_at" : "2016-01-04T15:31:39.788Z"
      },
      "author" : {
         "state" : "active",
         "web_url" : "https://gitlab.example.com/root",
         "avatar_url" : null,
         "username" : "root",
         "id" : 1,
         "name" : "Administrator"
      },
      "description" : "Omnis vero earum sunt corporis dolor et placeat.",
      "state" : "closed",
      "iid" : 1,
      "assignees" : [{
         "avatar_url" : null,
         "web_url" : "https://gitlab.example.com/lennie",
         "state" : "active",
         "username" : "lennie",
         "id" : 9,
         "name" : "Dr. Luella Kovacek"
      }],
      "assignee" : {
         "avatar_url" : null,
         "web_url" : "https://gitlab.example.com/lennie",
         "state" : "active",
         "username" : "lennie",
         "id" : 9,
         "name" : "Dr. Luella Kovacek"
      },
      "type" : "ISSUE",
      "labels" : ["foo", "bar"],
      "upvotes": 4,
      "downvotes": 0,
      "merge_requests_count": 0,
      "id" : 41,
      "title" : "Ut commodi ullam eos dolores perferendis nihil sunt.",
      "updated_at" : "2016-01-04T15:31:46.176Z",
      "created_at" : "2016-01-04T15:31:46.176Z",
      "closed_at" : "2016-01-05T15:31:46.176Z",
      "closed_by" : {
         "state" : "active",
         "web_url" : "https://gitlab.example.com/root",
         "avatar_url" : null,
         "username" : "root",
         "id" : 1,
         "name" : "Administrator"
      },
      "user_notes_count": 1,
      "due_date": "2016-07-22",
      "imported": false,
      "imported_from": "none",
      "web_url": "http://gitlab.example.com/my-group/my-project/issues/1",
      "references": {
        "short": "#1",
        "relative": "#1",
        "full": "my-group/my-project#1"
      },
      "time_stats": {
         "time_estimate": 0,
         "total_time_spent": 0,
         "human_time_estimate": null,
         "human_total_time_spent": null
      },
      "has_tasks": true,
      "task_status": "10 of 15 tasks completed",
      "confidential": false,
      "discussion_locked": false,
      "issue_type": "issue",
      "severity": "UNKNOWN",
      "_links":{
         "self":"http://gitlab.example.com/api/v4/projects/4/issues/41",
         "notes":"http://gitlab.example.com/api/v4/projects/4/issues/41/notes",
         "award_emoji":"http://gitlab.example.com/api/v4/projects/4/issues/41/award_emoji",
         "project":"http://gitlab.example.com/api/v4/projects/4",
         "closed_as_duplicate_of": "http://gitlab.example.com/api/v4/projects/1/issues/75"
      },
      "task_completion_status":{
         "count":0,
         "completed_count":0
      }
   }`
)
