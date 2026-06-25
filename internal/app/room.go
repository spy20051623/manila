package app

import (
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"manila/internal/ai"
	"manila/internal/model"
	"manila/internal/rules"
)

const (
	defaultRoomID       = "room-1"
	tutorialRoomID      = "tutorial"
	tutorialRoomName    = "新手教学"
	operationTimeout    = 60 * time.Second
	offlineSeatTimeout  = 60 * time.Second
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
	ID                   string
	Name                 string
	OwnerToken           string
	CreatedAt            time.Time
	Closed               bool
	Status               model.RoomStatus
	Participants         map[string]*roomParticipant
	Seats                map[int]*roomSeat
	ActiveGameID         string
	CloseAt              time.Time
	CloseReason          string
	TempAI               map[int]bool
	PendingAI            map[int]bool
	RoundNumber          int
	HumanActionThisRound bool
	TimeoutPlayer        int
	TimeoutEventSeq      int
	TimeoutKind          string
	TimeoutDeadline      time.Time
	timeoutTimer         *time.Timer
	closeTimer           *time.Timer
	offlineTimers        map[string]*time.Timer
}

type gameRoomSnapshot struct {
	RoomID string
	Name   string
	Seats  []gameSeatSnapshot
}

type gameSeatSnapshot struct {
	PlayerID int
	Type     model.SeatType
	Token    string
	Name     string
}

func newRoomState(id string, name string, ownerToken string) roomState {
	seats := map[int]*roomSeat{}
	for _, playerID := range model.PlayerOrder {
		seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
	}
	return roomState{
		ID:            id,
		Name:          name,
		OwnerToken:    ownerToken,
		CreatedAt:     time.Now(),
		Status:        model.RoomStatusWaiting,
		Participants:  map[string]*roomParticipant{},
		Seats:         seats,
		TempAI:        map[int]bool{},
		PendingAI:     map[int]bool{},
		offlineTimers: map[string]*time.Timer{},
	}
}

func (s *Service) JoinLobby(name string) (string, error) {
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
	s.participants[token] = &roomParticipant{Token: token, Name: cleanName}
	s.roomMu.Unlock()
	s.emitRoomChange()
	return token, nil
}

func (s *Service) JoinRoom(name string) (string, error) {
	token, err := s.JoinLobby(name)
	if err != nil {
		return "", err
	}
	_ = s.JoinExistingRoom(defaultRoomID, token)
	return token, nil
}

func (s *Service) RenameRoomParticipant(token string, name string) error {
	s.roomMu.Lock()
	participant := s.participantByTokenLocked(token)
	if participant == nil {
		s.roomMu.Unlock()
		return fmt.Errorf("invalid token")
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

func (s *Service) CreateRoom(token string) (model.RoomView, error) {
	s.roomMu.Lock()
	participant := s.participantByTokenLocked(token)
	if participant == nil {
		s.roomMu.Unlock()
		return model.RoomView{}, fmt.Errorf("invalid token")
	}
	id := fmt.Sprintf("room-%d", s.nextRoom)
	name := fmt.Sprintf("房间 %d", s.nextRoom)
	s.nextRoom++
	room := newRoomState(id, name, token)
	room.Participants[token] = participant
	s.rooms[id] = &room
	view := s.roomViewLocked(room.ID, token)
	s.roomMu.Unlock()
	s.emitRoomChange()
	return view, nil
}

func (s *Service) JoinExistingRoom(roomID string, token string) error {
	s.roomMu.Lock()
	room := s.rooms[roomID]
	participant := s.participants[token]
	if room == nil || room.Closed {
		s.roomMu.Unlock()
		return fmt.Errorf("room not found")
	}
	if participant == nil {
		s.roomMu.Unlock()
		return fmt.Errorf("invalid token")
	}
	room.Participants[token] = participant
	s.roomMu.Unlock()
	s.emitRoomChange()
	return nil
}

func (s *Service) ConnectRoomParticipant(token string) bool {
	s.roomMu.Lock()
	participant := s.participants[token]
	if participant == nil {
		s.roomMu.Unlock()
		return false
	}
	wasOffline := !participant.Online
	participant.Connections++
	participant.Online = true
	for _, room := range s.rooms {
		s.stopOfflineReleaseLocked(room, token)
	}
	s.roomMu.Unlock()
	if wasOffline {
		s.emitRoomChange()
	}
	return true
}

func (s *Service) DisconnectRoomParticipant(token string) {
	automationRooms := []string{}
	s.roomMu.Lock()
	participant := s.participantByTokenLocked(token)
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
	for _, room := range s.rooms {
		playerID := s.playerIDForTokenLocked(room, token)
		if room.Status == model.RoomStatusWaiting && playerID != 0 {
			if room.Seats[playerID].Ready {
				room.Seats[playerID].Ready = false
			}
			s.scheduleOfflineReleaseLocked(room, token)
		}
		if room.Status == model.RoomStatusInProgress && playerID != 0 {
			automationRooms = append(automationRooms, room.ID)
		}
	}
	s.roomMu.Unlock()
	s.emitRoomChange()
	for _, roomID := range automationRooms {
		go s.processRoomAutomation(roomID)
	}
}

func (s *Service) LobbyView(token string) (model.LobbyView, error) {
	s.roomMu.Lock()
	view := model.LobbyView{
		Participant:   s.participantViewLocked(token, ""),
		SuggestedName: s.defaultPlayerNameLocked(),
	}
	view.Rooms = append(view.Rooms, tutorialRoomSummary())
	rooms := make([]*roomState, 0, len(s.rooms))
	for _, room := range s.rooms {
		if !room.Closed {
			rooms = append(rooms, room)
		}
	}
	sort.Slice(rooms, func(i, j int) bool {
		return rooms[i].CreatedAt.Before(rooms[j].CreatedAt)
	})
	for _, room := range rooms {
		view.Rooms = append(view.Rooms, s.roomSummaryLocked(room, token))
	}
	s.roomMu.Unlock()
	view.CompletedGames = s.completedGameSummaries()
	return view, nil
}

func tutorialRoomSummary() model.RoomSummary {
	return model.RoomSummary{
		RoomID:           tutorialRoomID,
		Name:             tutorialRoomName,
		Status:           model.RoomStatusWaiting,
		HumanPlayerCount: 1,
		AIPlayerCount:    3,
		SeatCount:        4,
		IsTutorial:       true,
	}
}

func (s *Service) RoomViewInRoom(roomID string, token string) (model.RoomView, error) {
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil || room.Closed {
		s.roomMu.Unlock()
		return model.RoomView{}, fmt.Errorf("room not found")
	}
	view := s.roomViewLocked(roomID, token)
	s.roomMu.Unlock()
	view.CompletedGames = s.completedGameSummaries()
	return view, nil
}

func (s *Service) RoomView(token string) (model.RoomView, error) {
	return s.RoomViewInRoom(defaultRoomID, token)
}

func (s *Service) completedGameSummaries() []model.CompletedGameSummary {
	refs := s.store.List()
	summaries := make([]model.CompletedGameSummary, 0, len(refs))
	for _, ref := range refs {
		ref.Mu.Lock()
		g := ref.Game
		if g != nil && g.Status == model.StatusEnded {
			scores := append([]model.Score(nil), g.FinalScores...)
			summaries = append(summaries, model.CompletedGameSummary{
				GameID:      g.ID,
				RoundNumber: g.RoundNumber,
				EventSeq:    g.EventSeq,
				Scores:      scores,
			})
		}
		ref.Mu.Unlock()
	}
	sort.Slice(summaries, func(i, j int) bool {
		left := gameNumber(summaries[i].GameID)
		right := gameNumber(summaries[j].GameID)
		if left != right {
			return left > right
		}
		return summaries[i].GameID > summaries[j].GameID
	})
	return summaries
}

func gameNumber(id string) int {
	value := strings.TrimPrefix(id, "game-")
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

func (s *Service) RoomPlayerID(token string) int {
	return s.RoomPlayerIDInRoom(defaultRoomID, token)
}

func (s *Service) RoomPlayerIDInRoom(roomID string, token string) int {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	return s.playerIDForTokenLocked(s.rooms[roomID], token)
}

func (s *Service) GamePlayerID(token string, gameID string) int {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	room := s.roomForGameLocked(gameID)
	return s.playerIDForTokenLocked(room, token)
}

func (s *Service) IsActiveRoomGame(gameID string) bool {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	room := s.roomForGameLocked(gameID)
	return room != nil && !room.Closed && room.ActiveGameID == gameID && room.Status == model.RoomStatusInProgress
}

func (s *Service) ClaimSeat(token string, playerID int) error {
	return s.ClaimSeatInRoom(defaultRoomID, token, playerID)
}

func (s *Service) ClaimSeatInRoom(roomID string, token string, playerID int) error {
	changed, err := s.withWaitingParticipant(roomID, token, func(room *roomState) error {
		if !validPlayerID(playerID) {
			return fmt.Errorf("invalid player id")
		}
		if room.Seats[playerID].Type != model.SeatTypeEmpty {
			return fmt.Errorf("seat is occupied")
		}
		current := s.playerIDForTokenLocked(room, token)
		if current != 0 && room.Seats[current].Ready {
			return fmt.Errorf("cancel ready before switching seats")
		}
		if current != 0 {
			room.Seats[current] = &roomSeat{Type: model.SeatTypeEmpty}
		}
		room.Seats[playerID] = &roomSeat{Type: model.SeatTypeHuman, Token: token}
		s.stopOfflineReleaseLocked(room, token)
		if participant := s.participantByTokenLocked(token); participant != nil && !participant.Online {
			s.scheduleOfflineReleaseLocked(room, token)
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
	return s.LeaveSeatInRoom(defaultRoomID, token)
}

func (s *Service) LeaveSeatInRoom(roomID string, token string) error {
	changed, err := s.withWaitingParticipant(roomID, token, func(room *roomState) error {
		playerID := s.playerIDForTokenLocked(room, token)
		if playerID == 0 {
			return fmt.Errorf("not seated")
		}
		if room.Seats[playerID].Ready {
			return fmt.Errorf("cancel ready before leaving seat")
		}
		room.Seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
		s.stopOfflineReleaseLocked(room, token)
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
	return s.ReadyInRoom(defaultRoomID, token)
}

func (s *Service) ReadyInRoom(roomID string, token string) error {
	changed, err := s.withWaitingParticipant(roomID, token, func(room *roomState) error {
		playerID := s.playerIDForTokenLocked(room, token)
		if playerID == 0 {
			return fmt.Errorf("choose a seat before ready")
		}
		room.Seats[playerID].Ready = true
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.maybeStartRoomGame(roomID)
		s.emitRoomChange()
	}
	return nil
}

func (s *Service) Unready(token string) error {
	return s.UnreadyInRoom(defaultRoomID, token)
}

func (s *Service) UnreadyInRoom(roomID string, token string) error {
	changed, err := s.withWaitingParticipant(roomID, token, func(room *roomState) error {
		playerID := s.playerIDForTokenLocked(room, token)
		if playerID == 0 {
			return fmt.Errorf("not seated")
		}
		room.Seats[playerID].Ready = false
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
	return s.PlaceAIInRoom(defaultRoomID, token, playerID)
}

func (s *Service) PlaceAIInRoom(roomID string, token string, playerID int) error {
	changed, err := s.withWaitingParticipant(roomID, token, func(room *roomState) error {
		if !s.canManageAILocked(room, token) {
			return fmt.Errorf("cannot manage AI")
		}
		if !validPlayerID(playerID) {
			return fmt.Errorf("invalid player id")
		}
		if room.Seats[playerID].Type != model.SeatTypeEmpty {
			return fmt.Errorf("seat is occupied")
		}
		room.Seats[playerID] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.maybeStartRoomGame(roomID)
		s.emitRoomChange()
	}
	return nil
}

func (s *Service) RemoveAI(token string, playerID int) error {
	return s.RemoveAIInRoom(defaultRoomID, token, playerID)
}

func (s *Service) RemoveAIInRoom(roomID string, token string, playerID int) error {
	changed, err := s.withWaitingParticipant(roomID, token, func(room *roomState) error {
		if !s.canManageAILocked(room, token) {
			return fmt.Errorf("cannot manage AI")
		}
		if !validPlayerID(playerID) {
			return fmt.Errorf("invalid player id")
		}
		if room.Seats[playerID].Type != model.SeatTypeAI {
			return fmt.Errorf("seat does not contain AI")
		}
		room.Seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
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
	return s.ApplyRoomActionInRoom(defaultRoomID, token, action)
}

func (s *Service) ApplyRoomActionInRoom(roomID string, token string, action model.Action) (*model.Game, error) {
	playerID := s.RoomPlayerIDInRoom(roomID, token)
	if playerID == 0 || action.PlayerID != playerID {
		return nil, fmt.Errorf("token cannot act for player")
	}
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil || room.Closed || room.Status != model.RoomStatusInProgress || room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return nil, fmt.Errorf("room is not in progress")
	}
	gameID := room.ActiveGameID
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
	s.markHumanAction(roomID)
	if g.Status == model.StatusEnded {
		s.finishActiveEndedRoomGame(g, false)
		return g, nil
	}
	s.emitGameChange(gameID)
	go s.processRoomAutomation(roomID)
	return g, nil
}

func (s *Service) ApplyRoomActionForGame(token string, gameID string, action model.Action) (*model.Game, error) {
	roomID, err := s.RoomIDForGame(gameID)
	if err != nil {
		return nil, err
	}
	return s.ApplyRoomActionInRoom(roomID, token, action)
}

func (s *Service) RoomLegalActions(token string) ([]model.LegalAction, int, error) {
	return s.RoomLegalActionsInRoom(defaultRoomID, token)
}

func (s *Service) RoomLegalActionsInRoom(roomID string, token string) ([]model.LegalAction, int, error) {
	playerID := s.RoomPlayerIDInRoom(roomID, token)
	if playerID == 0 {
		return []model.LegalAction{}, 0, nil
	}
	s.roomMu.Lock()
	room := s.rooms[roomID]
	gameID := ""
	status := model.RoomStatusWaiting
	if room != nil && !room.Closed {
		gameID = room.ActiveGameID
		status = room.Status
	}
	s.roomMu.Unlock()
	if status != model.RoomStatusInProgress || gameID == "" {
		return []model.LegalAction{}, 0, nil
	}
	return s.LegalActions(gameID, playerID)
}

func (s *Service) RoomAIStep(token string) (*model.Game, model.Action, error) {
	return s.RoomAIStepInRoom(defaultRoomID, token)
}

func (s *Service) RoomAIStepInRoom(roomID string, token string) (*model.Game, model.Action, error) {
	playerID := s.RoomPlayerIDInRoom(roomID, token)
	if playerID == 0 {
		return nil, model.Action{}, fmt.Errorf("not seated")
	}
	if g, action, ok, err := s.applyConfirmRoundForHumanSeat(roomID, playerID); ok || err != nil {
		if err != nil {
			return nil, model.Action{}, err
		}
		s.markHumanAction(roomID)
		if g.Status == model.StatusEnded {
			s.finishActiveEndedRoomGame(g, false)
		}
		s.emitGameChange(g.ID)
		go s.processRoomAutomation(roomID)
		return g, action, nil
	}
	g, action, err := s.applyAIForHumanSeat(roomID, playerID, false)
	if err != nil {
		return nil, model.Action{}, err
	}
	s.markHumanAction(roomID)
	if g.Status == model.StatusEnded {
		s.finishActiveEndedRoomGame(g, false)
		return g, action, nil
	}
	s.emitGameChange(g.ID)
	go s.processRoomAutomation(roomID)
	return g, action, nil
}

func (s *Service) RoomAIRound(token string) (*model.Game, error) {
	return s.RoomAIRoundInRoom(defaultRoomID, token)
}

func (s *Service) RoomAIRoundInRoom(roomID string, token string) (*model.Game, error) {
	playerID := s.RoomPlayerIDInRoom(roomID, token)
	if playerID == 0 {
		return nil, fmt.Errorf("not seated")
	}
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil || room.Closed || room.Status != model.RoomStatusInProgress || room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return nil, fmt.Errorf("room is not in progress")
	}
	gameID := room.ActiveGameID
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
		if room := s.rooms[roomID]; room != nil && !room.Closed {
			room.PendingAI[playerID] = true
			if advanced {
				s.activatePendingAILocked(room)
			}
		}
		s.roomMu.Unlock()
		s.markHumanAction(roomID)
		s.emitGameChange(gameID)
		go s.processRoomAutomation(roomID)
		return g, nil
	}
	if roomActorForPhase(ref.Game) != playerID {
		ref.Mu.Unlock()
		return nil, fmt.Errorf("not player %d turn", playerID)
	}
	ref.Mu.Unlock()

	s.roomMu.Lock()
	if room := s.rooms[roomID]; room != nil && !room.Closed {
		room.TempAI[playerID] = true
	}
	s.roomMu.Unlock()
	g, _, err := s.applyAIForHumanSeat(roomID, playerID, true)
	if err != nil {
		return nil, err
	}
	s.markHumanAction(roomID)
	if g.Status == model.StatusEnded {
		s.finishActiveEndedRoomGame(g, false)
		return g, nil
	}
	s.emitGameChange(g.ID)
	go s.processRoomAutomation(roomID)
	return g, nil
}

func (s *Service) applyConfirmRoundForHumanSeat(roomID string, playerID int) (*model.Game, model.Action, bool, error) {
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil || room.Closed || room.Status != model.RoomStatusInProgress || room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return nil, model.Action{}, false, nil
	}
	if room.Seats[playerID].Type != model.SeatTypeHuman {
		s.roomMu.Unlock()
		return nil, model.Action{}, false, fmt.Errorf("not a human seat")
	}
	gameID := room.ActiveGameID
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

func (s *Service) withWaitingParticipant(roomID string, token string, fn func(*roomState) error) (bool, error) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	room := s.rooms[roomID]
	if room == nil || room.Closed {
		return false, fmt.Errorf("room not found")
	}
	participant := s.participantByTokenLocked(token)
	if participant == nil {
		return false, fmt.Errorf("invalid token")
	}
	room.Participants[token] = participant
	if room.Status != model.RoomStatusWaiting {
		return false, fmt.Errorf("room is not waiting")
	}
	if err := fn(room); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) maybeStartRoomGame(roomID string) {
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil || room.Closed || room.Status != model.RoomStatusWaiting || !s.allSeatsReadyLocked(room) {
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
	room.Status = model.RoomStatusInProgress
	room.ActiveGameID = id
	room.CloseAt = time.Time{}
	room.CloseReason = ""
	room.TempAI = map[int]bool{}
	room.PendingAI = map[int]bool{}
	room.RoundNumber = g.RoundNumber
	room.HumanActionThisRound = false
	s.gameRooms[id] = roomID
	s.roomMu.Unlock()
	go s.processRoomAutomation(roomID)
}

func (s *Service) processRoomAutomation(roomID string) {
	for step := 0; step < maxAutomationSteps; step++ {
		gameID, changed, roomChanged, continueLoop := s.processRoomAutomationStep(roomID)
		if changed && gameID != "" {
			s.emitGameChange(gameID)
		}
		if roomChanged {
			s.emitRoomChange()
		}
		if !continueLoop {
			return
		}
	}
}

func (s *Service) processRoomAutomationStep(roomID string) (string, bool, bool, bool) {
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil || room.Closed || room.Status != model.RoomStatusInProgress || room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return "", false, false, false
	}
	gameID := room.ActiveGameID
	ref, ok := s.store.Get(gameID)
	if !ok {
		s.roomMu.Unlock()
		return "", false, false, false
	}
	ref.Mu.Lock()
	g := ref.Game
	if g.Status == model.StatusEnded {
		s.finishEndedRoomGameLocked(room, g)
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return gameID, true, true, false
	}
	if g.Phase == model.PhaseRoundReview {
		changed := false
		s.clearTempAIOnReviewLocked(room, g)
		if g.Round.ConfirmedPlayers == nil {
			g.Round.ConfirmedPlayers = map[int]bool{}
		}
		for _, playerID := range model.PlayerOrder {
			if room.Seats[playerID].Type == model.SeatTypeAI && !g.Round.ConfirmedPlayers[playerID] {
				_ = s.engine.ApplyAction(g, model.Action{PlayerID: playerID, Type: model.ActionConfirmRound})
				changed = true
			}
		}
		if g.Phase != model.PhaseRoundReview {
			s.afterRoundAdvancedLocked(room, g)
			if room.Status == model.RoomStatusInProgress {
				s.activatePendingAILocked(room)
			}
			ref.Mu.Unlock()
			s.roomMu.Unlock()
			return gameID, true, false, true
		}
		timeoutChanged := s.scheduleReviewTimeoutLocked(room, g)
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return gameID, changed || timeoutChanged, false, false
	}
	if g.RoundNumber > room.RoundNumber {
		s.afterRoundAdvancedLocked(room, g)
		if room.Status != model.RoomStatusInProgress {
			ref.Mu.Unlock()
			s.roomMu.Unlock()
			return gameID, true, true, false
		}
		s.activatePendingAILocked(room)
	}
	current := roomActorForPhase(g)
	if !validPlayerID(current) {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return gameID, false, false, false
	}
	seat := room.Seats[current]
	if seat.Type == model.SeatTypeAI || room.TempAI[current] {
		action, err := s.trainedActionLocked(g, current)
		if err != nil {
			ref.Mu.Unlock()
			s.roomMu.Unlock()
			return gameID, false, false, false
		}
		_ = s.engine.ApplyAction(g, action)
		if g.Status == model.StatusEnded {
			s.finishEndedRoomGameLocked(room, g)
			ref.Mu.Unlock()
			s.roomMu.Unlock()
			return gameID, true, true, false
		}
		s.clearTempAIOnReviewLocked(room, g)
		s.afterPossibleRoundChangeLocked(room, g)
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return gameID, true, false, true
	}
	timeoutChanged := s.scheduleHumanTimeoutLocked(room, g, current)
	ref.Mu.Unlock()
	s.roomMu.Unlock()
	return gameID, timeoutChanged, false, false
}

func (s *Service) applyAIForHumanSeat(roomID string, playerID int, allowRoundTakeover bool) (*model.Game, model.Action, error) {
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil || room.Closed || room.Status != model.RoomStatusInProgress || room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return nil, model.Action{}, fmt.Errorf("room is not in progress")
	}
	if room.Seats[playerID].Type != model.SeatTypeHuman {
		s.roomMu.Unlock()
		return nil, model.Action{}, fmt.Errorf("not a human seat")
	}
	gameID := room.ActiveGameID
	ref, ok := s.store.Get(gameID)
	if !ok {
		s.roomMu.Unlock()
		return nil, model.Action{}, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	if roomActorForPhase(ref.Game) != playerID {
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
		s.clearTempAIOnReviewLocked(room, ref.Game)
	} else if !allowRoundTakeover {
		delete(room.TempAI, playerID)
	}
	s.afterPossibleRoundChangeLocked(room, ref.Game)
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

func (s *Service) clearTempAIOnReviewLocked(room *roomState, g *model.Game) {
	if g.Phase == model.PhaseRoundReview && len(room.TempAI) > 0 {
		room.TempAI = map[int]bool{}
	}
}

func (s *Service) activatePendingAILocked(room *roomState) {
	if len(room.PendingAI) == 0 {
		return
	}
	if room.TempAI == nil {
		room.TempAI = map[int]bool{}
	}
	for playerID := range room.PendingAI {
		room.TempAI[playerID] = true
	}
	room.PendingAI = map[int]bool{}
}

func (s *Service) scheduleHumanTimeoutLocked(room *roomState, g *model.Game, playerID int) bool {
	if s.humanPlayerCountLocked(room) <= 1 {
		return s.stopTimeoutLocked(room)
	}
	eventSeq := g.EventSeq
	roomID := room.ID
	return s.scheduleTimeoutLocked(room, roomTimeoutAction, playerID, eventSeq, func() {
		s.applyTimeoutAI(roomID, playerID, eventSeq)
	})
}

func (s *Service) scheduleReviewTimeoutLocked(room *roomState, g *model.Game) bool {
	if s.humanPlayerCountLocked(room) <= 1 {
		return s.stopTimeoutLocked(room)
	}
	hasUnconfirmed := false
	for _, playerID := range model.PlayerOrder {
		seat := room.Seats[playerID]
		if seat.Type != model.SeatTypeHuman || g.Round.ConfirmedPlayers[playerID] {
			continue
		}
		hasUnconfirmed = true
		break
	}
	if !hasUnconfirmed {
		if room.TimeoutKind == roomTimeoutReview {
			return s.stopTimeoutLocked(room)
		}
		return false
	}
	roomID := room.ID
	eventSeq := g.EventSeq
	return s.scheduleTimeoutLocked(room, roomTimeoutReview, 0, eventSeq, func() {
		s.applyReviewTimeout(roomID, eventSeq)
	})
}

func (s *Service) scheduleTimeoutLocked(room *roomState, kind string, playerID int, eventSeq int, fn func()) bool {
	if room.TimeoutKind == kind && room.TimeoutPlayer == playerID && room.TimeoutEventSeq == eventSeq {
		return false
	}
	if room.timeoutTimer != nil {
		room.timeoutTimer.Stop()
	}
	room.TimeoutKind = kind
	room.TimeoutPlayer = playerID
	room.TimeoutEventSeq = eventSeq
	room.TimeoutDeadline = time.Now().Add(operationTimeout)
	room.timeoutTimer = time.AfterFunc(operationTimeout, fn)
	return true
}

func (s *Service) stopTimeoutLocked(room *roomState) bool {
	if room == nil {
		return false
	}
	changed := room.TimeoutKind != "" || room.TimeoutPlayer != 0 || room.TimeoutEventSeq != 0 || !room.TimeoutDeadline.IsZero()
	if room.timeoutTimer != nil {
		room.timeoutTimer.Stop()
		room.timeoutTimer = nil
	}
	room.TimeoutKind = ""
	room.TimeoutPlayer = 0
	room.TimeoutEventSeq = 0
	room.TimeoutDeadline = time.Time{}
	return changed
}

func (s *Service) applyTimeoutAI(roomID string, playerID int, eventSeq int) {
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil || room.Closed || room.Status != model.RoomStatusInProgress || room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return
	}
	gameID := room.ActiveGameID
	ref, ok := s.store.Get(gameID)
	if !ok {
		s.roomMu.Unlock()
		return
	}
	ref.Mu.Lock()
	if roomActorForPhase(ref.Game) != playerID || ref.Game.EventSeq != eventSeq || room.Seats[playerID].Type != model.SeatTypeHuman {
		ref.Mu.Unlock()
		s.roomMu.Unlock()
		return
	}
	action, err := s.trainedActionLocked(ref.Game, playerID)
	if err == nil {
		_ = s.engine.ApplyAction(ref.Game, action)
		if ref.Game.Status == model.StatusEnded {
			s.finishEndedRoomGameLocked(room, ref.Game)
		} else {
			s.afterPossibleRoundChangeLocked(room, ref.Game)
		}
	}
	s.stopTimeoutLocked(room)
	ref.Mu.Unlock()
	s.roomMu.Unlock()
	s.emitGameChange(gameID)
	go s.processRoomAutomation(roomID)
}

func (s *Service) applyReviewTimeout(roomID string, eventSeq int) {
	changed := false
	continueAutomation := false
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil || room.Closed || room.Status != model.RoomStatusInProgress || room.ActiveGameID == "" {
		s.roomMu.Unlock()
		return
	}
	gameID := room.ActiveGameID
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
		seat := room.Seats[playerID]
		if seat.Type != model.SeatTypeHuman || g.Round.ConfirmedPlayers[playerID] {
			continue
		}
		if err := s.engine.ApplyAction(g, model.Action{PlayerID: playerID, Type: model.ActionConfirmRound}); err == nil {
			changed = true
		}
	}
	if g.Phase != model.PhaseRoundReview {
		s.afterRoundAdvancedLocked(room, g)
		if room.Status == model.RoomStatusInProgress {
			s.activatePendingAILocked(room)
			continueAutomation = true
		}
	}
	s.stopTimeoutLocked(room)
	ref.Mu.Unlock()
	s.roomMu.Unlock()
	if changed {
		s.emitGameChange(gameID)
	}
	if continueAutomation {
		go s.processRoomAutomation(roomID)
	}
}

func (s *Service) markHumanAction(roomID string) {
	s.roomMu.Lock()
	if room := s.rooms[roomID]; room != nil && !room.Closed {
		room.HumanActionThisRound = true
		s.stopTimeoutLocked(room)
	}
	s.roomMu.Unlock()
}

func (s *Service) afterPossibleRoundChangeLocked(room *roomState, g *model.Game) {
	if g.RoundNumber > room.RoundNumber {
		s.afterRoundAdvancedLocked(room, g)
		if room.Status == model.RoomStatusInProgress {
			s.activatePendingAILocked(room)
		}
	}
}

func (s *Service) afterRoundAdvancedLocked(room *roomState, g *model.Game) {
	if s.humanPlayerCountLocked(room) > 1 && !room.HumanActionThisRound {
		s.setClosingLocked(room, roomCloseNoneActive, 0)
		return
	}
	room.RoundNumber = g.RoundNumber
	room.HumanActionThisRound = false
}

func (s *Service) setClosingLocked(room *roomState, reason string, delay time.Duration) {
	room.Status = model.RoomStatusClosing
	room.CloseReason = reason
	room.CloseAt = time.Now().Add(delay)
	room.TempAI = map[int]bool{}
	room.PendingAI = map[int]bool{}
	s.stopTimeoutLocked(room)
	if room.closeTimer != nil {
		room.closeTimer.Stop()
	}
	roomID := room.ID
	room.closeTimer = time.AfterFunc(delay, func() {
		s.resetClosedRoom(roomID)
	})
}

func (s *Service) resetClosedRoom(roomID string) {
	s.roomMu.Lock()
	if room := s.rooms[roomID]; room != nil {
		s.resetClosedRoomLocked(room)
	}
	s.roomMu.Unlock()
	s.emitRoomChange()
}

func (s *Service) resetClosedRoomLocked(room *roomState) {
	s.stopTimeoutLocked(room)
	if room.closeTimer != nil {
		room.closeTimer.Stop()
		room.closeTimer = nil
	}
	s.stopAllOfflineReleasesLocked(room)
	participants := room.Participants
	*room = newRoomState(room.ID, room.Name, room.OwnerToken)
	room.Participants = participants
}

func (s *Service) finishActiveEndedRoomGame(g *model.Game, emitRoom bool) bool {
	if g == nil || g.Status != model.StatusEnded {
		return false
	}
	s.roomMu.Lock()
	room := s.roomForGameLocked(g.ID)
	if room == nil || room.Status != model.RoomStatusInProgress || room.ActiveGameID != g.ID {
		s.roomMu.Unlock()
		return false
	}
	s.finishEndedRoomGameLocked(room, g)
	s.roomMu.Unlock()
	s.emitGameChange(g.ID)
	if emitRoom {
		s.emitRoomChange()
	}
	return true
}

func (s *Service) FinishActiveRoomGame(g *model.Game) bool {
	return s.finishActiveEndedRoomGame(g, false)
}

func (s *Service) finishEndedRoomGameLocked(room *roomState, g *model.Game) {
	s.stopTimeoutLocked(room)
	if room.closeTimer != nil {
		room.closeTimer.Stop()
		room.closeTimer = nil
	}
	s.snapshotRoomForGameLocked(room, room.ActiveGameID)
	delete(s.gameRooms, room.ActiveGameID)
	room.Status = model.RoomStatusWaiting
	room.ActiveGameID = ""
	room.CloseAt = time.Time{}
	room.CloseReason = ""
	room.TempAI = map[int]bool{}
	room.PendingAI = map[int]bool{}
	room.RoundNumber = 0
	room.HumanActionThisRound = false
	s.stopAllOfflineReleasesLocked(room)
	for _, playerID := range model.PlayerOrder {
		room.Seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
	}
}

func (s *Service) AdminCloseRoom(roomID string, token string) error {
	s.roomMu.Lock()
	if token != s.adminToken {
		s.roomMu.Unlock()
		return fmt.Errorf("admin token required")
	}
	room := s.rooms[roomID]
	if room == nil || room.Closed {
		s.roomMu.Unlock()
		return fmt.Errorf("room not found")
	}
	s.stopTimeoutLocked(room)
	if room.closeTimer != nil {
		room.closeTimer.Stop()
	}
	s.stopAllOfflineReleasesLocked(room)
	if room.ActiveGameID != "" {
		s.snapshotRoomForGameLocked(room, room.ActiveGameID)
		delete(s.gameRooms, room.ActiveGameID)
	}
	room.Closed = true
	delete(s.rooms, roomID)
	s.roomMu.Unlock()
	s.emitRoomChange()
	return nil
}

func (s *Service) snapshotRoomForGameLocked(room *roomState, gameID string) {
	if room == nil || gameID == "" {
		return
	}
	s.gameSnapshots[gameID] = gameRoomSnapshot{
		RoomID: room.ID,
		Name:   room.Name,
		Seats:  s.roomSeatSnapshotsLocked(room),
	}
}

func (s *Service) roomSeatSnapshotsLocked(room *roomState) []gameSeatSnapshot {
	seats := make([]gameSeatSnapshot, 0, len(model.PlayerOrder))
	for _, id := range model.PlayerOrder {
		seat := room.Seats[id]
		name := ""
		if seat.Type == model.SeatTypeHuman {
			if participant := s.participantForSeatLocked(room, seat.Token); participant != nil {
				name = participant.Name
			}
		}
		seats = append(seats, gameSeatSnapshot{
			PlayerID: id,
			Type:     seat.Type,
			Token:    seat.Token,
			Name:     name,
		})
	}
	return seats
}

func (s *Service) scheduleOfflineReleaseLocked(room *roomState, token string) {
	if room == nil || token == "" {
		return
	}
	if room.offlineTimers == nil {
		room.offlineTimers = map[string]*time.Timer{}
	}
	s.stopOfflineReleaseLocked(room, token)
	roomID := room.ID
	room.offlineTimers[token] = time.AfterFunc(offlineSeatTimeout, func() {
		s.releaseOfflineSeat(roomID, token)
	})
}

func (s *Service) stopOfflineReleaseLocked(room *roomState, token string) {
	if room == nil || room.offlineTimers == nil {
		return
	}
	if timer := room.offlineTimers[token]; timer != nil {
		timer.Stop()
		delete(room.offlineTimers, token)
	}
}

func (s *Service) stopAllOfflineReleasesLocked(room *roomState) {
	if room == nil {
		return
	}
	for token, timer := range room.offlineTimers {
		if timer != nil {
			timer.Stop()
		}
		delete(room.offlineTimers, token)
	}
}

func (s *Service) releaseOfflineSeat(roomID string, token string) {
	changed := false
	s.roomMu.Lock()
	room := s.rooms[roomID]
	if room == nil {
		s.roomMu.Unlock()
		return
	}
	delete(room.offlineTimers, token)
	participant := s.participantByTokenLocked(token)
	if participant != nil && !participant.Online && room.Status == model.RoomStatusWaiting {
		playerID := s.playerIDForTokenLocked(room, token)
		if playerID != 0 {
			room.Seats[playerID] = &roomSeat{Type: model.SeatTypeEmpty}
			changed = true
		}
	}
	s.roomMu.Unlock()
	if changed {
		s.emitRoomChange()
	}
}

func (s *Service) roomViewLocked(roomID string, token string) model.RoomView {
	room := s.rooms[roomID]
	if room == nil {
		return model.RoomView{Status: model.RoomStatusWaiting}
	}
	playerID := s.playerIDForTokenLocked(room, token)
	view := model.RoomView{
		RoomID:           room.ID,
		Name:             room.Name,
		Status:           room.Status,
		Participant:      s.participantViewLocked(token, room.ID),
		GameID:           room.ActiveGameID,
		CloseReason:      room.CloseReason,
		SuggestedName:    s.defaultPlayerNameLocked(),
		HumanPlayerCount: s.humanPlayerCountLocked(room),
	}
	view.Participant.PlayerID = playerID
	view.Seats = s.roomSeatsViewLocked(room, token)
	if room.Status == model.RoomStatusClosing && !room.CloseAt.IsZero() {
		remaining := int(time.Until(room.CloseAt).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		view.ClosingSeconds = remaining
	}
	if playerID != 0 && room.Status == model.RoomStatusWaiting {
		seat := room.Seats[playerID]
		view.CanReady = seat.Type == model.SeatTypeHuman && !seat.Ready
		view.CanCancelReady = seat.Type == model.SeatTypeHuman && seat.Ready
		view.CanLeaveSeat = seat.Type == model.SeatTypeHuman && !seat.Ready
	}
	if room.Status == model.RoomStatusWaiting {
		view.CanManageAI = s.canManageAILocked(room, token)
	}
	view.CanAIForCurrent = s.canAIForCurrentLocked(room, playerID)
	return view
}

func (s *Service) roomSeatsViewLocked(room *roomState, token string) []model.RoomSeat {
	seats := make([]model.RoomSeat, 0, len(model.PlayerOrder))
	playerID := s.playerIDForTokenLocked(room, token)
	for _, id := range model.PlayerOrder {
		seat := room.Seats[id]
		seatName := ""
		seatOnline := true
		if seat.Type == model.SeatTypeHuman {
			if participant := s.participantForSeatLocked(room, seat.Token); participant != nil {
				seatName = participant.Name
				seatOnline = participant.Online
			}
		}
		seats = append(seats, model.RoomSeat{
			PlayerID: id,
			Type:     seat.Type,
			Name:     seatName,
			Ready:    seat.Ready,
			Online:   seatOnline,
			IsYou:    playerID == id,
		})
	}
	return seats
}

func (s *Service) participantViewLocked(token string, roomID string) model.RoomParticipant {
	if token == s.adminToken && token != "" {
		return model.RoomParticipant{Joined: true, Name: "管理员", IsAdmin: true}
	}
	participant := s.participantByTokenLocked(token)
	if participant == nil {
		return model.RoomParticipant{}
	}
	playerID := 0
	if roomID != "" {
		playerID = s.playerIDForTokenLocked(s.rooms[roomID], token)
	}
	return model.RoomParticipant{Joined: true, PlayerID: playerID, Name: participant.Name}
}

func (s *Service) roomSummaryLocked(room *roomState, token string) model.RoomSummary {
	humans := 0
	ais := 0
	seats := 0
	for _, seat := range room.Seats {
		if seat.Type != model.SeatTypeEmpty {
			seats++
		}
		if seat.Type == model.SeatTypeHuman {
			humans++
		}
		if seat.Type == model.SeatTypeAI {
			ais++
		}
	}
	_, member := room.Participants[token]
	return model.RoomSummary{
		RoomID:           room.ID,
		Name:             room.Name,
		Status:           room.Status,
		GameID:           room.ActiveGameID,
		HumanPlayerCount: humans,
		AIPlayerCount:    ais,
		SeatCount:        seats,
		IsMember:         member,
		IsOwner:          room.OwnerToken == token && token != "",
		CanAdminClose:    token == s.adminToken && token != "",
	}
}

func (s *Service) canAIForCurrentLocked(room *roomState, playerID int) bool {
	if room == nil || playerID == 0 || room.Status != model.RoomStatusInProgress || room.ActiveGameID == "" {
		return false
	}
	ref, ok := s.store.Get(room.ActiveGameID)
	if !ok {
		return false
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	if ref.Game.Phase == model.PhaseRoundReview {
		return ref.Game.Round.ConfirmedPlayers == nil || !ref.Game.Round.ConfirmedPlayers[playerID]
	}
	return roomActorForPhase(ref.Game) == playerID
}

func roomActorForPhase(g *model.Game) int {
	switch g.Phase {
	case model.PhaseHarborMasterBuyShare, model.PhaseHarborMasterSelectGoods, model.PhaseHarborMasterSetShips:
		return g.HarborMaster
	default:
		return g.CurrentPlayer
	}
}

func (s *Service) allSeatsReadyLocked(room *roomState) bool {
	hasHuman := false
	for _, id := range model.PlayerOrder {
		seat := room.Seats[id]
		if seat.Type == model.SeatTypeEmpty {
			return false
		}
		if seat.Type == model.SeatTypeHuman {
			hasHuman = true
			if !seat.Ready {
				return false
			}
		}
	}
	return hasHuman
}

func (s *Service) playerIDForTokenLocked(room *roomState, token string) int {
	if room == nil || !s.validTokenLocked(token) {
		return 0
	}
	for _, id := range model.PlayerOrder {
		seat := room.Seats[id]
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
	if token == s.adminToken {
		return true
	}
	if _, ok := s.participants[token]; ok {
		return true
	}
	for _, room := range s.rooms {
		if _, ok := room.Participants[token]; ok {
			return true
		}
	}
	return false
}

func (s *Service) canManageAILocked(room *roomState, token string) bool {
	if room == nil || token == s.adminToken || s.participantByTokenLocked(token) == nil || room.Status != model.RoomStatusWaiting {
		return false
	}
	room.Participants[token] = s.participantByTokenLocked(token)
	playerID := s.playerIDForTokenLocked(room, token)
	if playerID == 0 {
		return true
	}
	seat := room.Seats[playerID]
	return seat.Type == model.SeatTypeHuman && !seat.Ready
}

func (s *Service) humanPlayerCountLocked(room *roomState) int {
	count := 0
	if room == nil {
		return count
	}
	for _, id := range model.PlayerOrder {
		if room.Seats[id].Type == model.SeatTypeHuman {
			count++
		}
	}
	return count
}

func (s *Service) participantForSeatLocked(room *roomState, token string) *roomParticipant {
	if participant := s.participants[token]; participant != nil {
		return participant
	}
	return room.Participants[token]
}

func (s *Service) participantByTokenLocked(token string) *roomParticipant {
	if participant := s.participants[token]; participant != nil {
		return participant
	}
	for _, room := range s.rooms {
		if participant := room.Participants[token]; participant != nil {
			return participant
		}
	}
	return nil
}

func (s *Service) roomForGameLocked(gameID string) *roomState {
	if roomID := s.gameRooms[gameID]; roomID != "" {
		return s.rooms[roomID]
	}
	for _, room := range s.rooms {
		if room.ActiveGameID == gameID {
			return room
		}
	}
	return nil
}

func (s *Service) RoomIDForGame(gameID string) (string, error) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	room := s.roomForGameLocked(gameID)
	if room == nil || room.Closed {
		return "", fmt.Errorf("room not found for game")
	}
	return room.ID, nil
}

func (s *Service) RoomContextForGame(gameID string, token string) (string, string, []model.RoomSeat, error) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	if room := s.roomForGameLocked(gameID); room != nil && !room.Closed {
		return room.ID, room.Name, s.roomSeatsViewLocked(room, token), nil
	}
	if snapshot, ok := s.gameSnapshots[gameID]; ok {
		return snapshot.RoomID, snapshot.Name, s.snapshotSeatsViewLocked(snapshot, token), nil
	}
	return "", "", nil, fmt.Errorf("room not found for game")
}

func (s *Service) RoomTimeoutForGame(gameID string) (model.RoomTimeoutView, bool) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	room := s.roomForGameLocked(gameID)
	if room == nil || room.Closed || room.TimeoutKind == "" || room.TimeoutDeadline.IsZero() {
		return model.RoomTimeoutView{}, false
	}
	remaining := time.Until(room.TimeoutDeadline)
	remainingSeconds := 0
	remainingMillis := 0
	if remaining > 0 {
		remainingSeconds = int((remaining + time.Second - time.Nanosecond) / time.Second)
		remainingMillis = int(remaining / time.Millisecond)
	}
	return model.RoomTimeoutView{
		Kind:             room.TimeoutKind,
		PlayerID:         room.TimeoutPlayer,
		EventSeq:         room.TimeoutEventSeq,
		DurationSeconds:  int(operationTimeout / time.Second),
		RemainingSeconds: remainingSeconds,
		RemainingMillis:  remainingMillis,
		HumanPlayerCount: s.humanPlayerCountLocked(room),
	}, true
}

func (s *Service) snapshotSeatsViewLocked(snapshot gameRoomSnapshot, token string) []model.RoomSeat {
	seats := make([]model.RoomSeat, 0, len(snapshot.Seats))
	for _, seat := range snapshot.Seats {
		name := seat.Name
		online := true
		if seat.Type == model.SeatTypeHuman {
			if participant := s.participantByTokenLocked(seat.Token); participant != nil {
				name = participant.Name
				online = participant.Online
			}
		}
		seats = append(seats, model.RoomSeat{
			PlayerID: seat.PlayerID,
			Type:     seat.Type,
			Name:     name,
			Ready:    true,
			Online:   online,
			IsYou:    token != "" && token == seat.Token,
		})
	}
	return seats
}

func (s *Service) emitRoomChange() {
	s.roomMu.Lock()
	notify := s.notify
	s.roomMu.Unlock()
	if notify != nil {
		notify()
	}
}

func (s *Service) emitGameChange(gameID string) {
	if gameID == "" {
		return
	}
	s.roomMu.Lock()
	notify := s.notifyGame
	s.roomMu.Unlock()
	if notify != nil {
		notify(gameID)
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
	for token, participant := range s.participants {
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
		for _, participant := range s.participants {
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
