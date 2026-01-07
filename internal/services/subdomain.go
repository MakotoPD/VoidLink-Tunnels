package services

import (
	"bufio"
	"crypto/rand"
	"math/big"
	"os"
	"strings"
	"sync"
)

type SubdomainService struct {
	words []string
	mu    sync.RWMutex
}

func NewSubdomainService(wordlistPath string) (*SubdomainService, error) {
	s := &SubdomainService{}
	
	if err := s.loadWords(wordlistPath); err != nil {
		// Use default words if file not found
		s.words = defaultWords
	}
	
	return s, nil
}

func (s *SubdomainService) loadWords(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		word := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if len(word) >= 3 && len(word) <= 10 && isAlpha(word) {
			s.words = append(s.words, word)
		}
	}
	
	return scanner.Err()
}

func isAlpha(s string) bool {
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// Generate creates a unique subdomain with 1-3 random words
func (s *SubdomainService) Generate() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Randomly pick 2 or 3 words (more variety)
	numWords := 2
	if n, _ := rand.Int(rand.Reader, big.NewInt(2)); n.Int64() == 1 {
		numWords = 3
	}

	parts := make([]string, numWords)
	for i := 0; i < numWords; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(s.words))))
		if err != nil {
			return "", err
		}
		parts[i] = s.words[idx.Int64()]
	}

	return strings.Join(parts, "-"), nil
}

// Default word list (memorable, easy to type)
var defaultWords = []string{
	// Animals
	"wolf", "bear", "fox", "hawk", "eagle", "tiger", "lion", "shark", "dragon", "phoenix",
	"raven", "falcon", "panther", "cobra", "viper", "lynx", "horse", "deer", "owl", "crow",
	// Nature
	"fire", "ice", "storm", "thunder", "shadow", "light", "dark", "frost", "flame", "wind",
	"stone", "iron", "steel", "gold", "silver", "crystal", "ember", "ash", "cloud", "star",
	// Actions
	"swift", "brave", "bold", "silent", "wild", "fierce", "rapid", "steady", "mystic", "cosmic",
	"ancient", "prime", "alpha", "omega", "epic", "noble", "royal", "grand", "elite", "ultra",
	// Objects
	"blade", "arrow", "shield", "spear", "crown", "helm", "forge", "tower", "gate", "bridge",
	"castle", "fortress", "citadel", "realm", "domain", "kingdom", "empire", "legion", "order", "guild",
	// Colors
	"red", "blue", "green", "black", "white", "gray", "purple", "orange", "crimson", "azure",
	// Tech
	"cyber", "neon", "pixel", "byte", "data", "core", "nexus", "vertex", "matrix", "grid",
	// Misc
	"zen", "apex", "nova", "pulse", "wave", "flux", "spark", "bolt", "rush", "blast",
	"quest", "hunt", "raid", "siege", "strike", "force", "power", "might", "fury", "rage",
}
