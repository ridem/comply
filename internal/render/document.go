package render

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"os/exec"

	"github.com/pkg/errors"
	"github.com/strongdm/comply/internal/config"
	"github.com/strongdm/comply/internal/model"
	"gopkg.in/yaml.v2"
)

type DocumentMetadata struct {
	Title         string   `yaml:"title"`
	Author        string   `yaml:"author"`
	Date          string   `yaml:"date"`
	IncludeHeader bool     `yaml:"header-includes"`
	HeadContent   string   `yaml:"head-content"`
	FootContent   string   `yaml:"foot-content"`
	ListOfFigures bool     `yaml:"lof"`
	Tables        bool     `yaml:"tables"`
	IncludeBefore []string `yaml:"include-before"`
}

func createTable(name string, header []string, rows [][]string) string {
	stringRows := ""
	for _, row := range rows {
		stringRows += fmt.Sprintf("%s|%s\n", row[0], row[1])
	}
	return fmt.Sprintf(`%s
  ---|:-----
  %s
  : %s
  
  `, strings.Join(header, "|"), stringRows, name)
}

func getMetadata(pol *model.Document) DocumentMetadata {
	cfg := config.Config()
	metadata := DocumentMetadata{
		Title:         pol.Name,
		Author:        cfg.Name,
		IncludeHeader: true,
		HeadContent:   pol.Name,
		FootContent:   fmt.Sprintf("%s confidential %d", pol.Name, time.Now().Year()),
		Date:          fmt.Sprintf("%s %d", pol.ModifiedAt.Month().String(), pol.ModifiedAt.Year()),
	}
	includeBefore := []string{}

	if len(pol.Satisfies) > 0 {
		var rows []([]string)
		for standard, keys := range pol.Satisfies {
			rows = append(rows, []string{standard, strings.Join(keys, ", ")})
		}
		satisfiesTable := createTable("Criteria satisfaction", []string{"Standard", "Criteria Satisfied"}, rows)
		includeBefore = append(includeBefore, satisfiesTable)
	}

	if len(pol.Revisions) > 0 {
		var rows []([]string)
		for _, rev := range pol.Revisions {
			rows = append(rows, []string{rev.Date, rev.Comment})
		}
		revisionTable := createTable("Document history", []string{"Date", "Comment"}, rows)
		includeBefore = append(includeBefore, revisionTable)
	}

	if len(pol.Owner) > 0 {
		documentOwner := fmt.Sprintf("Policy Owner: %s\n\n", pol.Owner)
		includeBefore = append(includeBefore, documentOwner)
	}

	metadata.IncludeBefore = includeBefore
	return metadata
}

// TODO: refactor and eliminate duplication among narrative, policy renderers
func renderToFilesystem(wg *sync.WaitGroup, semaphore chan struct{}, data *renderData, doc *model.Document, live bool) {
	// only files that have been touched
	if !isNewer(doc.FullPath, doc.ModifiedAt) {
		return
	}
	recordModified(doc.FullPath, doc.ModifiedAt)

	wg.Add(1)
	go func(p *model.Document) error {
		defer wg.Done()

		semaphore <- struct{}{} // Lock
		defer func() {
			<-semaphore // Unlock
		}()

		pdfFolder := config.Config().PDFFolder

		pdfRelativePath := p.OutputFilename
		if pdfFolder != "" {
			pdfRelativePath = pdfFolder + "/" + p.OutputFilename
		}

		markdownPath := filepath.Join(".", "output", pdfRelativePath+".md")

		// save preprocessed markdown
		err := preprocessDoc(data, p, markdownPath)
		if err != nil {
			fmt.Printf("Unable to preprocess %s (%s) - %v\n", p.Name, p.Acronym, err)
			return err
		}

		err = pandoc(pdfRelativePath)
		if err != nil {
			fmt.Printf("Unable to generate a PDF for %s (%s) - %v\n", p.Name, p.Acronym, err)
			return err
		}

		// remove preprocessed markdown
		err = os.Remove(markdownPath)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(config.ProjectRoot(), p.FullPath)
		if err != nil {
			rel = p.FullPath
		}
		fmt.Printf("%s -> %s\n", rel, pdfRelativePath)

		return nil

	}(doc)
}

func getGitApprovalInfo(pol *model.Document) (string, error) {
	cfg := config.Config()

	// if no approved branch specified in config.yaml, then nothing gets added to the document
	if cfg.ApprovedBranch == "" {
		return "", nil
	}

	// Decide whether we are on the git branch that contains the approved policies
	gitBranchArgs := []string{"symbolic-ref", "--short", "HEAD"}
	gitBranchCmd := exec.Command("git", gitBranchArgs...)
	gitBranchInfo, err := gitBranchCmd.CombinedOutput()

	var testBranch string
	if err != nil {
		// return "", errors.Wrap(err, "error looking up git branch")
		// It is gonna break if we're in a "detached HEAD" mode
		testBranch = cfg.ApprovedBranch
	} else {
		testBranch = strings.TrimSpace(fmt.Sprintf("%s", gitBranchInfo))
	}

	// if on a different branch than the approved branch, then nothing gets added to the document
	if strings.Compare(testBranch, cfg.ApprovedBranch) != 0 {
		return "", nil
	}

	// Grab information related to commit, so that we can put approval information in the document
	gitArgs := []string{"log", "-n", "1", "--date=format:%b %d %Y", "--pretty=format:%ad", "--", pol.FullPath}
	cmd := exec.Command("git", gitArgs...)
	gitApprovalInfo, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.Wrap(err, "error looking up git committer and author data")
	}

	return string(gitApprovalInfo), nil
}

func preprocessDoc(data *renderData, pol *model.Document, fullPath string) error {
	var w bytes.Buffer
	bodyTemplate, err := template.New("body").Parse(pol.Body)
	if err != nil {
		w.WriteString(fmt.Sprintf("# Error processing template:\n\n%s\n", err.Error()))
	} else {
		bodyTemplate.Execute(&w, data)
	}
	body := w.String()

	metadata := getMetadata(pol)

	ymlData, _ := yaml.Marshal(&metadata)

	frontmatter := fmt.Sprintf("---\n%s\n---", ymlData)

	doc := fmt.Sprintf("%s\n%s",
		frontmatter,
		body,
	)
	err = ioutil.WriteFile(fullPath, []byte(doc), os.FileMode(0644))
	if err != nil {
		return errors.Wrap(err, "unable to write preprocessed policy to disk")
	}
	return nil
}
