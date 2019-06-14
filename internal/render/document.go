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
)

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
	cfg := config.Config()

	var w bytes.Buffer
	bodyTemplate, err := template.New("body").Parse(pol.Body)
	if err != nil {
		w.WriteString(fmt.Sprintf("# Error processing template:\n\n%s\n", err.Error()))
	} else {
		bodyTemplate.Execute(&w, data)
	}
	body := w.String()

	revisionTable := ""
	satisfiesTable := ""

	// ||Date|Comment|
	// |---+------|
	// | 4 Jan 2018 | Initial Version |
	// Table: Document history

	if len(pol.Satisfies) > 0 {
		rows := ""
		for standard, keys := range pol.Satisfies {
			rows += fmt.Sprintf("| %s | %s |\n", standard, strings.Join(keys, ", "))
		}
		satisfiesTable = fmt.Sprintf("Criteria satisfaction\n\n|Standard|Criteria Satisfied|\n|-------+--------------------------------------------|\n%s\n\n", rows)
	}

	if err != nil {
		fmt.Println(err)
		return err
	}

	if len(pol.Revisions) > 0 {
		rows := ""
		for _, rev := range pol.Revisions {
			rows += fmt.Sprintf("| %s | %s |\n", rev.Date, rev.Comment)
		}
		revisionTable = fmt.Sprintf("Document history\n\n|Date|Comment|\n|---+--------------------------------------------|\n%s\n\n", rows)
	}

	doc := fmt.Sprintf(`%% %s
%% %s
%% %s

---
header-includes: yes
head-content: "%s"
foot-content: "%s confidential %d"
lof: True
tables: True
include-before:
  - %q
  - %q
---

%s`,
		pol.Name,
		cfg.Name,
		fmt.Sprintf("%s %d", pol.ModifiedAt.Month().String(), pol.ModifiedAt.Year()),
		pol.Name,
		cfg.Name,
		time.Now().Year(),
		satisfiesTable,
		revisionTable,
		body,
	)
	err = ioutil.WriteFile(fullPath, []byte(doc), os.FileMode(0644))
	if err != nil {
		return errors.Wrap(err, "unable to write preprocessed policy to disk")
	}
	return nil
}
