package app

import (
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"manila/internal/ai"
	"manila/internal/model"
	"manila/internal/rules"
)

const (
	operationTimeout    = 20 * time.Second
	offlineSeatTimeout  = 20 * time.Second
	maxAutomationSteps  = 300
	maxPlayerNameRunes  = 12
	roomCloseNoneActive = "整轮没有真人执行操作"
	roomTimeoutAction   = "action"
	roomTimeoutReview   = "review"
)

type roomParticipant struct {
	Token       string
	Name        string
	Connections int
	Online      bool
}

type roomSeat struct {
	Type  model.SeatType
	Token string
	Ready bool
}

type roomState struct {
	Status               model.RoomStatus
	Participants         map[string]*roomParticipant
	Seats                map[int]*roomSeat
	ActiveGameID         string
	CloseAt              time.Time
	CloseReason          string
	LastSettlement       *model.RoomSettlement
	TempAI               map[int]bool
	PendingAI            map[int]bool
	RoundNumber          int
	HumanActionThisRound bool
	TimeoutPlayer        int
	TimeoutEventSeq      int
	TimeoutKind          string
}

func newRoomState() roomState {
	seats := map[int]*roomSeat{}
	for _, playerID := range model.PlayerOrder {
		seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
	}
	return roomState{
		Status:       model.RoomStatusWaiting,
		Participants: map[string]*roomParticipant{},
		Seats:        seats,
		TempAI:       map[int]bool{},
		PendingAI:    map[int]bool{},
	}
}

func (s *Service) JoinRoom(name string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.roomMu.Lock()
	cleanName := s.cleanPlayerNameLocked(name)
	if err := s.validatePlayerNameLocked(cleanName, ""); err != nil {
		s.roomMu.Unlock()
		return "", err
	}
	s.room.Participants[token] = &roomParticipant{Token: token, Name: cleanName}
	s.roomMu.Unlock()
	s.emitRoomChange()
	return token, nil
}

func (s *Service) RenameRoomParticipant(token string, name string) error {
	s.roomMu.Lock()
	participant := s.room.Participants[token]
	if participant == nil {
		s.roomMu.Unlock()
		return fmt.Errorf("invalid room token")
	}
	cleanName := strings.TrimSpace(name)
	if err := s.validatePlayerNameLocked(cleanName, token); err != nil {
		s.roomMu.Unlock()
		return err
	}
	participant.Name = cleanName
	s.roomMu.Unlock()
	s.emitRoomChange()
	return nil
}

func (s *Service) ConnectRoomParticipant(token string) bool {
	s.roomMu.Lock()
	participant := s.room.Participants[token]
	if participant == nil {
		s.roomMu.Unlock()
		return false
	}
	wasOffline := !participant.Online
	participant.Connections++
	participant.Online = true
	s.stopOfflineReleaseLocked(token)
	s.roomMu.Unlock()
	if wasOffline {
		s.emitRoomChange()
	}
	return true
}

func (s *Service) DisconnectRoomParticipant(token string) {
	needsAutomation := false
	s.roomMu.Lock()
	participant := s.room.Participants[token]
	if participant == nil {
		s.roomMu.Unlock()
		return
	}
	if participant.Connections > 0 {
		participant.Connections--
	}
	if participant.Connections > 0 {
		s.roomMu.Unlock()
		return
	}
	if !participant.Online {
		s.roomMu.Unlock()
		return
	}
	participant.Online = false
	playerID := s.playerIDForTokenLocked(token)
	if s.room.Status == model.RoomStatusWaiting && playerID != 0 {
		if s.room.Seats[playerID].Ready {
			s.room.Seats[playerID].Ready = false
		}
		s.scheduleOfflineReleaseLocked(token)
	}
	if s.room.Status == model.RoomStatusInProgress {
		needsAutomation = true
	}
	s.roomMu.Unlock()
	s.emitRoomChange()
	if needsAutomation {
		go s.processRoomAutomation()
	}
}

func (s *Service) RoomView(token string) (model.RoomView, error) {
	s.roomMu.Lock()
	view := s.roomViewLocked(token)
	gameID := s.room.ActiveGameID
	s.roomMu.Unlock()
	if gameID != "" {
		if g, err := s.GetGame(gameID); err == nil {
			if g.Status == model.StatusEnded && s.finishActiveEndedRoomGame(g) {
				s.roomMu.Lock()
				view = s.roomViewLocked(token)
				s.roomMu.Unlock()
				return view, nil
			}
			view.Game = g
			view.EventSeq = g.EventSeq
		}
	}
	return view, nil
}

func (s *Service) RoomPlayerID(token string) int {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	return s.playerIDForTokenLocked(token)
}

func (s *Service) ClaimSeat(token string, playerID int) error {
	changed, err := s.withWaitingParticipant(token, func() error {
		if !validPlayerID(playerID) {
			return fmt.Errorf("invalid player id")
		}
		if s.room.Seats[playerID].Type != model.SeatTypeEmpty {
			return fmt.Errorf("seat is occupied")
		}
		current := s.playerIDForTokenLocked(token)
		if current != 0 && s.room.Seats[current].Ready {
			return fmt.Errorf("cancel ready before switching seats")
		}
		if current != 0 {
			s.room.Seats[current] = &roomSeat{Type: model.SeatTypeEmpty}
		}
		s.room.Seats[playerID] = &roomSeat{Type: model.SeatTypeHuman, Token: token}
		s.stopOfflineReleaseLocked(token)
		if participant := s.room.Participants[token]; participant != nil && !participant.Online {
			s.scheduleOfflineReleaseLocked(token)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.emitRoomChange()
	}
	return nil
}

func (s *Service) LeaveSeat(token string) error {
	changed, err := s.withWaitingParticipant(token, func() error {
		playerID := s.playerIDForTokenLocked(token)
		if playerID == 0 {
			return fmt.Errorf("not seated")
		}
		if s.room.Seats[playerID].Ready {
			return fmt.Errorf("cancel ready before leaving seat")
		}
		s.room.Seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
		s.stopOfflineReleaseLocked(token)
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.emitRoomChange()
	}
	return nil
}

func (s *Service) Ready(token string) error {
	changed, err := s.withWaitingParticipant(token, func() error {
		playerID := s.playerIDForTokenLocked(token)
		if playerID == 0 {
			return fmt.Errorf("choose a seat before ready")
		}
		s.room.Seats[playerID].Ready = true
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.maybeStartRoomGame()
		s.emitRoomChange()
	}
	return nil
}

func (s *Service) Unready(token string) error {
	changed, err := s.withWaitingParticipant(token, func() error {
		playerID := s.playerIDForTokenLocked(token)
		if playerID == 0 {
			return fmt.Errorf("not seated")
		}
		s.room.Seats[playerID].Ready = false
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.emitRoomChange()
	}
	return nil
}

func (s *Service) PlaceAI(token string, playerID int) error {
	changed, err := s.withWaitingParticipant(token, func() error {
		if s.playerIDForTokenLocked(token) == 0 {
			return fmt.Errorf("choose a seat before managing AI")
		}
		if !validPlayerID(playerID) {
			return fmt.Errorf("invalid player id")
		}
		if s.room.Seats[playerID].Type != model.SeatTypeEmpty {
			return fmt.Errorf("seat is occupied")
		}
		s.room.Seats[playerID] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.maybeStartRoomGame()
		s.emitRoomChange()
	}
	return nil
}

func (s *Service) RemoveAI(token string, playerID int) error {
	changed, err := s.withWaitingParticipant(token, func() error {
		if s.playerIDForTokenLocked(token) == 0 {
			return fmt.Errorf("choose a seat before managing AI")
		}
		if !validPlayerID(playerID) {
			return fmt.Errorf("invalid player id")
		}
		if s.room.Seats[playerID].Type != model.SeatTypeAI {
			return fmt.Errorf("seat does not contain AI")
		}
		s.room.Seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.emitRoomChange()
	}
	return nil
}

func (s *Service) ApplyRoomAction(token string, action model.Action) (*model.Game, error) {
	playerID := s.RoomPlayerID(token)
	if playerID == 0 || action.PlayerID != playerID {
		return nil, fmt.Errorf("token cannot act for player")
	}
	s.roomMu.Lock()
	if s.room.Status != model.RoomStatusInProgress || s.room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return nil, fmt.Errorf("room is not in progress")
	}
	gameID := s.room.ActiveGameID
	s.roomMu.Unlock()

	ref, ok := s.store.Get(gameID)
	if !ok {
		return nil, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	action.GameID = gameID
	err := s.engine.ApplyAction(ref.Game, action)
	g := ref.Game
	ref.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	s.markHumanAction()
	if g.Status == model.StatusEnded {
		s.finishActiveEndedRoomGame(g)
		s.emitRoomChange()
		return g, nil
	}
	s.emitRoomChange()
	go s.processRoomAutomation()
	return g, nil
}

func (s *Service) RoomLegalActions(token string) ([]model.LegalAction, int, error) {
	playerID := s.RoomPlayerID(token)
	if playerID == 0 {
		return []model.LegalAction{}, 0, nil
	}
	s.roomMu.Lock()
	gameID := s.room.ActiveGameID
	status := s.room.Status
	s.roomMu.Unlock()
	if status != model.RoomStatusInProgress || gameID == "" {
		return []model.LegalAction{}, 0, nil
	}
	return s.LegalActions(gameID, playerID)
}

func (s *Service) RoomAIStep(token string) (*model.Game, model.Action, error) {
	playerID := s.RoomPlayerID(token)
	if playerID == 0 {
		return nil, model.Action{}, fmt.Errorf("not seated")
	}
	if g, action, ok, err := s.applyConfirmRoundForHumanSeat(playerID); ok || err != nil {
		if err != nil {
			return nil, model.Action{}, err
		}
		s.markHumanAction()
		if g.Status == model.StatusEnded {
			s.finishActiveEndedRoomGame(g)
		}
		s.emitRoomChange()
		go s.processRoomAutomation()
		return g, action, nil
	}
	g, action, err := s.applyAIForHumanSeat(playerID, false)
	if err != nil {
		return nil, model.Action{}, err
	}
	s.markHumanAction()
	if g.Status == model.StatusEnded {
		s.finishActiveEndedRoomGame(g)
		s.emitRoomChange()
		return g, action, nil
	}
	s.emitRoomChange()
	go s.processRoomAutomation()
	return g, action, nil
}

func (s *Service) RoomAIRound(token string) (*model.Game, error) {
	playerID := s.RoomPlayerID(token)
	if playerID == 0 {
		return nil, fmt.Errorf("not seated")
	}
	s.roomMu.Lock()
	if s.room.Status != model.RoomStatusInProgress || s.room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return nil, fmt.Errorf("room is not in progress")
	}
	gameID := s.room.ActiveGameID
	s.roomMu.Unlock()

	ref, ok := s.store.Get(gameID)
	if !ok {
		return nil, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	if ref.Game.Phase == model.PhaseRoundReview {
		if ref.Game.Round.ConfirmedPlayers == nil || !ref.Game.Round.ConfirmedPlayers[playerID] {
			if err := s.engine.ApplyAction(ref.Game, model.Action{PlayerID: playerID, Type: model.ActionConfirmRound}); err != nil {
				ref.Mu.Unlock()
				return nil, err
			}
		}
		g := ref.Game
		advanced := g.Phase != model.PhaseRoundReview
		ref.Mu.Unlock()
		s.roomMu.Lock()
		s.room.PendingAI[playerID] = true
		if advanced {
			s.activatePendingAILocked()
		}
		s.roomMu.Unlock()
		s.markHumanAction()
		s.emitRoomChange()
		go s.processRoomAutomation()
		return g, nil
	}
	if ref.Game.CurrentPlayer != playerID {
		ref.Mu.Unlock()
		return nil, fmt.Errorf("not player %d turn", playerID)
	}
	ref.Mu.Unlock()

	s.roomMu.Lock()
	s.room.TempAI[playerID] = true
	s.roomMu.Unlock()
	g, _, err := s.applyAIForHumanSeat(playerID, true)
	if err != nil {
		return nil, err
	}
	s.markHumanAction()
	if g.Status == model.StatusEnded {
		s.finishActiveEndedRoomGame(g)
		s.emitRoomChange()
		return g, nil
	}
	s.emitRoomChange()
	go s.processRoomAutomation()
	return g, nil
}

func (s *Service) applyConfirmRoundForHumanSeat(playerID int) (*model.Game, model.Action, bool, error) {
	s.roomMu.Lock()
	if s.room.Status != model.RoomStatusInProgress || s.room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return nil, model.Action{}, false, nil
	}
	if s.room.Seats[playerID].Type != model.SeatTypeHuman {
		s.roomMu.Unlock()
		return nil, model.Action{}, false, fmt.Errorf("not a human seat")
	}
	gameID := s.room.ActiveGameID
	ref, ok := s.store.Get(gameID)
	if !ok {
		s.roomMu.Unlock()
		return nil, model.Action{}, false, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	if ref.Game.Phase != model.PhaseRoundReview {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return nil, model.Action{}, false, nil
	}
	if ref.Game.Round.ConfirmedPlayers != nil && ref.Game.Round.ConfirmedPlayers[playerID] {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return ref.Game, model.Action{}, true, nil
	}
	action := model.Action{PlayerID: playerID, Type: model.ActionConfirmRound}
	if err := s.engine.ApplyAction(ref.Game, action); err != nil {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return nil, model.Action{}, true, err
	}
	g := ref.Game
	ref.Mu.Unlock()
	s.roomMu.Unlock()
	return g, action, true, nil
}

func (s *Service) withWaitingParticipant(token string, fn func() error) (bool, error) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	if !s.validTokenLocked(token) {
		return false, fmt.Errorf("invalid token")
	}
	if s.room.Status != model.RoomStatusWaiting {
		return false, fmt.Errorf("room is not waiting")
	}
	if err := fn(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) maybeStartRoomGame() {
	s.roomMu.Lock()
	if s.room.Status != model.RoomStatusWaiting || !s.allSeatsReadyLocked() {
		s.roomMu.Unlock()
		return
	}
	id := s.store.NextID()
	g := rules.NewGame(id, time.Now().UnixNano())
	if err := s.engine.StartGame(g); err != nil {
		s.roomMu.Unlock()
		return
	}
	s.store.Put(g)
	s.room.Status = model.RoomStatusInProgress
	s.room.ActiveGameID = id
	s.room.CloseAt = time.Time{}
	s.room.CloseReason = ""
	s.room.LastSettlement = nil
	s.room.TempAI = map[int]bool{}
	s.room.PendingAI = map[int]bool{}
	s.room.RoundNumber = g.RoundNumber
	s.room.HumanActionThisRound = false
	s.roomMu.Unlock()
	go s.processRoomAutomation()
}

func (s *Service) processRoomAutomation() {
	for step := 0; step < maxAutomationSteps; step++ {
		changed, continueLoop := s.processRoomAutomationStep()
		if changed {
			s.emitRoomChange()
		}
		if !continueLoop {
			return
		}
	}
}

func (s *Service) processRoomAutomationStep() (bool, bool) {
	s.roomMu.Lock()
	if s.room.Status != model.RoomStatusInProgress || s.room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return false, false
	}
	gameID := s.room.ActiveGameID
	ref, ok := s.store.Get(gameID)
	if !ok {
		s.roomMu.Unlock()
		return false, false
	}
	ref.Mu.Lock()
	g := ref.Game
	if g.Status == model.StatusEnded {
		s.finishEndedRoomGameLocked(g)
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return true, false
	}
	if g.Phase == model.PhaseRoundReview {
		changed := false
		s.clearTempAIOnReviewLocked(g)
		if g.Round.ConfirmedPlayers == nil {
			g.Round.ConfirmedPlayers = map[int]bool{}
		}
		for _, playerID := range model.PlayerOrder {
			if s.room.Seats[playerID].Type == model.SeatTypeAI && !g.Round.ConfirmedPlayers[playerID] {
				_ = s.engine.ApplyAction(g, model.Action{PlayerID: playerID, Type: model.ActionConfirmRound})
				changed = true
			}
		}
		if g.Phase != model.PhaseRoundReview {
			s.afterRoundAdvancedLocked(g)
			if s.room.Status == model.RoomStatusInProgress {
				s.activatePendingAILocked()
			}
			ref.Mu.Unlock()
			s.roomMu.Unlock()
			return true, true
		}
		s.scheduleReviewTimeoutLocked(g)
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return changed, false
	}
	if g.RoundNumber > s.room.RoundNumber {
		s.afterRoundAdvancedLocked(g)
		if s.room.Status != model.RoomStatusInProgress {
			ref.Mu.Unlock()
			s.roomMu.Unlock()
			return true, false
		}
		s.activatePendingAILocked()
	}
	current := g.CurrentPlayer
	if !validPlayerID(current) {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return false, false
	}
	seat := s.room.Seats[current]
	if seat.Type == model.SeatTypeAI || s.room.TempAI[current] {
		action, err := s.trainedActionLocked(g, current)
		if err != nil {
			ref.Mu.Unlock()
			s.roomMu.Unlock()
			return false, false
		}
		_ = s.engine.ApplyAction(g, action)
		if g.Status == model.StatusEnded {
			s.finishEndedRoomGameLocked(g)
			ref.Mu.Unlock()
			s.roomMu.Unlock()
			return true, false
		}
		s.clearTempAIOnReviewLocked(g)
		s.afterPossibleRoundChangeLocked(g)
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return true, true
	}
	s.scheduleHumanTimeoutLocked(g, current)
	ref.Mu.Unlock()
	s.roomMu.Unlock()
	return false, false
}

func (s *Service) applyAIForHumanSeat(playerID int, allowRoundTakeover bool) (*model.Game, model.Action, error) {
	s.roomMu.Lock()
	if s.room.Status != model.RoomStatusInProgress || s.room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return nil, model.Action{}, fmt.Errorf("room is not in progress")
	}
	if s.room.Seats[playerID].Type != model.SeatTypeHuman {
		s.roomMu.Unlock()
		return nil, model.Action{}, fmt.Errorf("not a human seat")
	}
	gameID := s.room.ActiveGameID
	ref, ok := s.store.Get(gameID)
	if !ok {
		s.roomMu.Unlock()
		return nil, model.Action{}, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	if ref.Game.CurrentPlayer != playerID {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return nil, model.Action{}, fmt.Errorf("not player %d turn", playerID)
	}
	action, err := s.trainedActionLocked(ref.Game, playerID)
	if err != nil {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return nil, model.Action{}, err
	}
	if err := s.engine.ApplyAction(ref.Game, action); err != nil {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return nil, model.Action{}, err
	}
	if ref.Game.Phase == model.PhaseRoundReview {
		s.clearTempAIOnReviewLocked(ref.Game)
	} else if !allowRoundTakeover {
		delete(s.room.TempAI, playerID)
	}
	s.afterPossibleRoundChangeLocked(ref.Game)
	g := ref.Game
	ref.Mu.Unlock()
	s.roomMu.Unlock()
	return g, action, nil
}

func (s *Service) trainedActionLocked(g *model.Game, playerID int) (model.Action, error) {
	genome, err := s.trainedGenomeForPlayer(g, playerID)
	if err != nil {
		return model.Action{}, err
	}
	agent := ai.NewEvolvableAgent(genome, g.Seed+int64(g.EventSeq+1)*7919+int64(playerID)*101)
	return agent.ChooseAction(s.engine, g, playerID)
}

func (s *Service) clearTempAIOnReviewLocked(g *model.Game) {
	if g.Phase == model.PhaseRoundReview && len(s.room.TempAI) > 0 {
		s.room.TempAI = map[int]bool{}
	}
}

func (s *Service) activatePendingAILocked() {
	if len(s.room.PendingAI) == 0 {
		return
	}
	if s.room.TempAI == nil {
		s.room.TempAI = map[int]bool{}
	}
	for playerID := range s.room.PendingAI {
		s.room.TempAI[playerID] = true
	}
	s.room.PendingAI = map[int]bool{}
}

func (s *Service) scheduleHumanTimeoutLocked(g *model.Game, playerID int) {
	if s.humanPlayerCountLocked() <= 1 {
		s.stopTimeoutLocked()
		return
	}
	s.scheduleTimeoutLocked(roomTimeoutAction, playerID, g.EventSeq, func() {
		s.applyTimeoutAI(playerID, g.EventSeq)
	})
}

func (s *Service) scheduleReviewTimeoutLocked(g *model.Game) {
	if s.humanPlayerCountLocked() <= 1 {
		s.stopTimeoutLocked()
		return
	}
	hasUnconfirmed := false
	for _, playerID := range model.PlayerOrder {
		seat := s.room.Seats[playerID]
		if seat.Type != model.SeatTypeHuman || g.Round.ConfirmedPlayers[playerID] {
			continue
		}
		hasUnconfirmed = true
		break
	}
	if !hasUnconfirmed {
		if s.room.TimeoutKind == roomTimeoutReview {
			s.stopTimeoutLocked()
		}
		return
	}
	s.scheduleTimeoutLocked(roomTimeoutReview, 0, g.EventSeq, func() {
		s.applyReviewTimeout(g.EventSeq)
	})
}

func (s *Service) scheduleTimeoutLocked(kind string, playerID int, eventSeq int, fn func()) {
	if s.room.TimeoutKind == kind && s.room.TimeoutPlayer == playerID && s.room.TimeoutEventSeq == eventSeq {
		return
	}
	if s.timeoutTimer != nil {
		s.timeoutTimer.Stop()
	}
	s.room.TimeoutKind = kind
	s.room.TimeoutPlayer = playerID
	s.room.TimeoutEventSeq = eventSeq
	s.timeoutTimer = time.AfterFunc(operationTimeout, fn)
}

func (s *Service) stopTimeoutLocked() {
	if s.timeoutTimer != nil {
		s.timeoutTimer.Stop()
		s.timeoutTimer = nil
	}
	s.room.TimeoutKind = ""
	s.room.TimeoutPlayer = 0
	s.room.TimeoutEventSeq = 0
}

func (s *Service) applyTimeoutAI(playerID int, eventSeq int) {
	s.roomMu.Lock()
	if s.room.Status != model.RoomStatusInProgress || s.room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return
	}
	gameID := s.room.ActiveGameID
	ref, ok := s.store.Get(gameID)
	if !ok {
		s.roomMu.Unlock()
		return
	}
	ref.Mu.Lock()
	if ref.Game.CurrentPlayer != playerID || ref.Game.EventSeq != eventSeq || s.room.Seats[playerID].Type != model.SeatTypeHuman {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return
	}
	action, err := s.trainedActionLocked(ref.Game, playerID)
	if err == nil {
		_ = s.engine.ApplyAction(ref.Game, action)
		if ref.Game.Status == model.StatusEnded {
			s.finishEndedRoomGameLocked(ref.Game)
		} else {
			s.afterPossibleRoundChangeLocked(ref.Game)
		}
	}
	s.stopTimeoutLocked()
	ref.Mu.Unlock()
	s.roomMu.Unlock()
	s.emitRoomChange()
	go s.processRoomAutomation()
}

func (s *Service) applyReviewTimeout(eventSeq int) {
	changed := false
	continueAutomation := false
	s.roomMu.Lock()
	if s.room.Status != model.RoomStatusInProgress || s.room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return
	}
	gameID := s.room.ActiveGameID
	ref, ok := s.store.Get(gameID)
	if !ok {
		s.roomMu.Unlock()
		return
	}
	ref.Mu.Lock()
	g := ref.Game
	if g.Phase != model.PhaseRoundReview || g.EventSeq != eventSeq {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return
	}
	if g.Round.ConfirmedPlayers == nil {
		g.Round.ConfirmedPlayers = map[int]bool{}
	}
	for _, playerID := range model.PlayerOrder {
		seat := s.room.Seats[playerID]
		if seat.Type != model.SeatTypeHuman || g.Round.ConfirmedPlayers[playerID] {
			continue
		}
		if err := s.engine.ApplyAction(g, model.Action{PlayerID: playerID, Type: model.ActionConfirmRound}); err == nil {
			changed = true
		}
	}
	if g.Phase != model.PhaseRoundReview {
		s.afterRoundAdvancedLocked(g)
		if s.room.Status == model.RoomStatusInProgress {
			s.activatePendingAILocked()
			continueAutomation = true
		}
	}
	s.stopTimeoutLocked()
	ref.Mu.Unlock()
	s.roomMu.Unlock()
	if changed {
		s.emitRoomChange()
	}
	if continueAutomation {
		go s.processRoomAutomation()
	}
}

func (s *Service) markHumanAction() {
	s.roomMu.Lock()
	s.room.HumanActionThisRound = true
	s.stopTimeoutLocked()
	s.roomMu.Unlock()
}

func (s *Service) afterPossibleRoundChangeLocked(g *model.Game) {
	if g.RoundNumber > s.room.RoundNumber {
		s.afterRoundAdvancedLocked(g)
		if s.room.Status == model.RoomStatusInProgress {
			s.activatePendingAILocked()
		}
	}
}

func (s *Service) afterRoundAdvancedLocked(g *model.Game) {
	if s.humanPlayerCountLocked() > 1 && !s.room.HumanActionThisRound {
		s.setClosingLocked(roomCloseNoneActive, 0)
		return
	}
	s.room.RoundNumber = g.RoundNumber
	s.room.HumanActionThisRound = false
}

func (s *Service) setClosingLocked(reason string, delay time.Duration) {
	s.room.Status = model.RoomStatusClosing
	s.room.CloseReason = reason
	s.room.CloseAt = time.Now().Add(delay)
	s.room.LastSettlement = nil
	s.room.TempAI = map[int]bool{}
	s.room.PendingAI = map[int]bool{}
	s.stopTimeoutLocked()
	if s.closeTimer != nil {
		s.closeTimer.Stop()
	}
	s.closeTimer = time.AfterFunc(delay, func() {
		s.resetClosedRoom()
	})
}

func (s *Service) resetClosedRoom() {
	s.roomMu.Lock()
	s.resetClosedRoomLocked()
	s.roomMu.Unlock()
	s.emitRoomChange()
}

func (s *Service) resetClosedRoomLocked() {
	s.stopTimeoutLocked()
	if s.closeTimer != nil {
		s.closeTimer.Stop()
		s.closeTimer = nil
	}
	s.stopAllOfflineReleasesLocked()
	participants := s.room.Participants
	s.room = newRoomState()
	s.room.Participants = participants
}

func (s *Service) finishActiveEndedRoomGame(g *model.Game) bool {
	if g == nil || g.Status != model.StatusEnded {
		return false
	}
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	if s.room.Status != model.RoomStatusInProgress || s.room.ActiveGameID != g.ID {
		return false
	}
	s.finishEndedRoomGameLocked(g)
	return true
}

func (s *Service) finishEndedRoomGameLocked(g *model.Game) {
	s.stopTimeoutLocked()
	if s.closeTimer != nil {
		s.closeTimer.Stop()
		s.closeTimer = nil
	}
	s.room.LastSettlement = &model.RoomSettlement{
		GameID:   g.ID,
		EventSeq: g.EventSeq,
		Scores:   scoresForSettlement(g),
	}
	s.room.Status = model.RoomStatusWaiting
	s.room.ActiveGameID = ""
	s.room.CloseAt = time.Time{}
	s.room.CloseReason = ""
	s.room.TempAI = map[int]bool{}
	s.room.PendingAI = map[int]bool{}
	s.room.RoundNumber = 0
	s.room.HumanActionThisRound = false
	for _, playerID := range model.PlayerOrder {
		seat := s.room.Seats[playerID]
		if seat.Type == model.SeatTypeHuman {
			seat.Ready = false
			if participant := s.room.Participants[seat.Token]; participant != nil && !participant.Online {
				s.scheduleOfflineReleaseLocked(seat.Token)
			}
			continue
		}
		s.room.Seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
	}
}

func (s *Service) scheduleOfflineReleaseLocked(token string) {
	if token == "" {
		return
	}
	if s.offlineTimers == nil {
		s.offlineTimers = map[string]*time.Timer{}
	}
	s.stopOfflineReleaseLocked(token)
	s.offlineTimers[token] = time.AfterFunc(offlineSeatTimeout, func() {
		s.releaseOfflineSeat(token)
	})
}

func (s *Service) stopOfflineReleaseLocked(token string) {
	if timer := s.offlineTimers[token]; timer != nil {
		timer.Stop()
		delete(s.offlineTimers, token)
	}
}

func (s *Service) stopAllOfflineReleasesLocked() {
	for token, timer := range s.offlineTimers {
		if timer != nil {
			timer.Stop()
		}
		delete(s.offlineTimers, token)
	}
}

func (s *Service) releaseOfflineSeat(token string) {
	changed := false
	s.roomMu.Lock()
	delete(s.offlineTimers, token)
	participant := s.room.Participants[token]
	if participant != nil && !participant.Online && s.room.Status == model.RoomStatusWaiting {
		playerID := s.playerIDForTokenLocked(token)
		if playerID != 0 {
			s.room.Seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
			changed = true
		}
	}
	s.roomMu.Unlock()
	if changed {
		s.emitRoomChange()
	}
}

func scoresForSettlement(g *model.Game) []model.Score {
	if len(g.FinalScores) > 0 {
		return append([]model.Score(nil), g.FinalScores...)
	}
	for i := len(g.Events) - 1; i >= 0; i-- {
		event := g.Events[i]
		if event.Type != "GameEnded" {
			continue
		}
		if scores, ok := event.Data["scores"].([]model.Score); ok {
			return append([]model.Score(nil), scores...)
		}
	}
	return nil
}

func (s *Service) roomViewLocked(token string) model.RoomView {
	playerID := s.playerIDForTokenLocked(token)
	participantName := ""
	if participant := s.room.Participants[token]; participant != nil {
		participantName = participant.Name
	}
	view := model.RoomView{
		Status:           s.room.Status,
		Participant:      model.RoomParticipant{Joined: s.validTokenLocked(token), PlayerID: playerID, Name: participantName},
		GameID:           s.room.ActiveGameID,
		CloseReason:      s.room.CloseReason,
		LastSettlement:   s.room.LastSettlement,
		SuggestedName:    s.defaultPlayerNameLocked(),
		HumanPlayerCount: s.humanPlayerCountLocked(),
	}
	for _, id := range model.PlayerOrder {
		seat := s.room.Seats[id]
		seatName := ""
		seatOnline := true
		if seat.Type == model.SeatTypeHuman {
			if participant := s.room.Participants[seat.Token]; participant != nil {
				seatName = participant.Name
				seatOnline = participant.Online
			}
		}
		view.Seats = append(view.Seats, model.RoomSeat{
			PlayerID: id,
			Type:     seat.Type,
			Name:     seatName,
			Ready:    seat.Ready,
			Online:   seatOnline,
			IsYou:    playerID == id,
		})
	}
	if s.room.Status == model.RoomStatusClosing && !s.room.CloseAt.IsZero() {
		remaining := int(time.Until(s.room.CloseAt).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		view.ClosingSeconds = remaining
	}
	if playerID != 0 && s.room.Status == model.RoomStatusWaiting {
		seat := s.room.Seats[playerID]
		view.CanManageAI = seat.Type == model.SeatTypeHuman && !seat.Ready
		view.CanReady = seat.Type == model.SeatTypeHuman && !seat.Ready
		view.CanCancelReady = seat.Type == model.SeatTypeHuman && seat.Ready
		view.CanLeaveSeat = seat.Type == model.SeatTypeHuman && !seat.Ready
	}
	view.CanAIForCurrent = s.canAIForCurrentLocked(playerID)
	return view
}

func (s *Service) canAIForCurrentLocked(playerID int) bool {
	if playerID == 0 || s.room.Status != model.RoomStatusInProgress || s.room.ActiveGameID == "" {
		return false
	}
	ref, ok := s.store.Get(s.room.ActiveGameID)
	if !ok {
		return false
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	if ref.Game.Phase == model.PhaseRoundReview {
		return ref.Game.Round.ConfirmedPlayers == nil || !ref.Game.Round.ConfirmedPlayers[playerID]
	}
	return ref.Game.CurrentPlayer == playerID
}

func (s *Service) allSeatsReadyLocked() bool {
	for _, id := range model.PlayerOrder {
		seat := s.room.Seats[id]
		if seat.Type == model.SeatTypeEmpty {
			return false
		}
		if seat.Type == model.SeatTypeHuman && !seat.Ready {
			return false
		}
	}
	return true
}

func (s *Service) playerIDForTokenLocked(token string) int {
	if !s.validTokenLocked(token) {
		return 0
	}
	for _, id := range model.PlayerOrder {
		seat := s.room.Seats[id]
		if seat.Type == model.SeatTypeHuman && seat.Token == token {
			return id
		}
	}
	return 0
}

func (s *Service) validTokenLocked(token string) bool {
	if token == "" {
		return false
	}
	_, ok := s.room.Participants[token]
	return ok
}

func (s *Service) humanPlayerCountLocked() int {
	count := 0
	for _, id := range model.PlayerOrder {
		if s.room.Seats[id].Type == model.SeatTypeHuman {
			count++
		}
	}
	return count
}

func (s *Service) emitRoomChange() {
	s.roomMu.Lock()
	notify := s.notify
	s.roomMu.Unlock()
	if notify != nil {
		notify()
	}
}

func validPlayerID(playerID int) bool {
	return playerID >= 1 && playerID <= 4
}

func (s *Service) cleanPlayerNameLocked(name string) string {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return s.defaultPlayerNameLocked()
	}
	return cleanName
}

func (s *Service) validatePlayerNameLocked(name string, exceptToken string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("player name is required")
	}
	if utf8.RuneCountInString(name) > maxPlayerNameRunes {
		return fmt.Errorf("player name must be at most %d characters", maxPlayerNameRunes)
	}
	for token, participant := range s.room.Participants {
		if token == exceptToken {
			continue
		}
		if strings.EqualFold(participant.Name, name) {
			return fmt.Errorf("player name is already used")
		}
	}
	return nil
}

func (s *Service) defaultPlayerNameLocked() string {
	for i := 1; ; i++ {
		name := fmt.Sprintf("Player %d", i)
		used := false
		for _, participant := range s.room.Participants {
			if strings.EqualFold(participant.Name, name) {
				used = true
				break
			}
		}
		if !used {
			return name
		}
	}
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := crand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
