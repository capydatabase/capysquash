package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
)

// BranchSafetyChecker checks if it's safe to squash on the current branch
type BranchSafetyChecker struct {
	protectedBranches []string
}

// NewBranchSafetyChecker creates a new branch safety checker
func NewBranchSafetyChecker() *BranchSafetyChecker {
	return &BranchSafetyChecker{
		protectedBranches: []string{"main", "master", "production", "prod"},
	}
}

// GetCurrentBranch gets the current git branch name
func (bsc *BranchSafetyChecker) GetCurrentBranch() (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")
	output, err := cmd.Output()
	if err != nil {
		// Not a git repository or git not available
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// IsProtectedBranch checks if the branch is a protected branch
func (bsc *BranchSafetyChecker) IsProtectedBranch(branch string) bool {
	branchLower := strings.ToLower(branch)
	for _, protected := range bsc.protectedBranches {
		if branchLower == protected {
			return true
		}
	}
	return false
}

// CheckBranchSafety checks if it's safe to squash and prompts user if needed
// Returns true if safe to proceed, false otherwise
func (bsc *BranchSafetyChecker) CheckBranchSafety(branchCheck, force bool) (bool, error) {
	// Get current branch
	currentBranch, err := bsc.GetCurrentBranch()
	if err != nil {
		// Not a git repository - allow squashing with warning
		if !force {
			fmt.Println(color.YellowString("⚠️  Warning: Not a git repository. Cannot verify branch safety."))
		}
		return true, nil
	}

	isProtected := bsc.IsProtectedBranch(currentBranch)

	// If --branch-check is enabled, enforce protected branch requirement
	if branchCheck && !isProtected {
		return false, fmt.Errorf("squashing is only allowed on protected branches (main, master, production)\nCurrent branch: %s\nUse --i-know-what-im-doing to override", currentBranch)
	}

	// If on a non-protected branch and not forced, show warning and prompt
	if !isProtected && !force {
		return bsc.promptUserForConfirmation(currentBranch)
	}

	return true, nil
}

// promptUserForConfirmation prompts the user for confirmation when squashing on non-protected branch
func (bsc *BranchSafetyChecker) promptUserForConfirmation(currentBranch string) (bool, error) {
	fmt.Println(color.RedString(`
╔════════════════════════════════════════════════════════════════╗
║  ⚠️  WARNING: You are about to squash migrations               ║
║                                                                ║
║  Current branch: %-45s ║
║                                                                ║
║  If other developers have open branches with new migrations,  ║
║  merging will create conflicts.                               ║
║                                                                ║
║  BEST PRACTICE:                                                ║
║  • Only squash on main/master branch                          ║
║  • Coordinate with your team before squashing                 ║
║  • Have everyone rebase after squashing                       ║
║                                                                ║
║  Use --branch-check to enforce this requirement               ║
║  Use --i-know-what-im-doing to skip this warning              ║
╚════════════════════════════════════════════════════════════════╝
`, currentBranch))

	fmt.Print("\nDo you want to continue? [yes/NO]: ")
	
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response == "yes" || response == "y" {
		return true, nil
	}

	return false, fmt.Errorf("squash cancelled by user")
}

// FormatBranchWarning formats a warning message about branch safety
func (bsc *BranchSafetyChecker) FormatBranchWarning(currentBranch string) string {
	isProtected := bsc.IsProtectedBranch(currentBranch)
	
	if isProtected {
		return color.GreenString("✓ On protected branch: %s", currentBranch)
	}
	
	return color.YellowString("⚠️  On non-protected branch: %s (consider squashing on main/master)", currentBranch)
}
