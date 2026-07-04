package app

import (
	"testing"
	"time"

	"manila/internal/model"
	"manila/internal/rules"
	"manila/internal/store"
)

func defaultRoomForTest(svc *Service) *roomState {
	room := newRoomState(defaultRoomID, "Test Room", "")
	svc.rooms[room.ID] = &room
	return &room
}

func TestFinishEndedRoomGameClearsSeatsAndReturnsToLobby(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	room := defaultRoomForTest(svc)

	scores := []model.Score{
		{PlayerID: 1, Wealth: 128, Rank: 1},
		{PlayerID: 2, Wealth: 96, Rank: 2},
		{PlayerID: 3, Wealth: 72, Rank: 3},
		{PlayerID: 4, Wealth: 64, Rank: 4},
	}

	svc.roomMu.Lock()
	room.Status = model.RoomStatusInProgress
	room.ActiveGameID = "game-1"
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice", Ready: true}
	room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob", Ready: true}
	room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.TempAI = map[int]bool{1: true}
	svc.finishEndedRoomGameLocked(room, &model.Game{ID: "game-1", EventSeq: 42, FinalScores: scores})
	svc.roomMu.Unlock()

	view, err := svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != model.RoomStatusWaiting {
		t.Fatalf("expected room to return to waiting, got %s", view.Status)
	}
	if view.GameID != "" {
		t.Fatalf("expected active game to be cleared, got %q", view.GameID)
	}
	if !view.Participant.Joined || view.Participant.PlayerID != 0 {
		t.Fatalf("expected Alice token to remain joined but unseated, got %+v", view.Participant)
	}
	if view.CanReady || view.CanLeaveSeat || !view.CanManageAI {
		t.Fatalf("expected Alice to manage AI but choose a seat before readying, got %+v", view)
	}
	seats := seatsByID(view.Seats)
	for _, playerID := range model.PlayerOrder {
		if seats[playerID].Type != model.SeatTypeEmpty {
			t.Fatalf("expected P%d seat to be cleared, got %+v", playerID, seats[playerID])
		}
	}
}

func TestRoomViewDoesNotFinalizeEndedActiveGame(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, rules.NewEngine())
	room := defaultRoomForTest(svc)
	g := &model.Game{
		ID:          "game-1",
		Status:      model.StatusEnded,
		EventSeq:    9,
		FinalScores: []model.Score{{PlayerID: 1, Wealth: 120, Rank: 1}},
	}
	st.Put(g)

	svc.roomMu.Lock()
	room.Status = model.RoomStatusInProgress
	room.ActiveGameID = "game-1"
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice", Ready: true}
	room.Seats[2] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.roomMu.Unlock()

	view, err := svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != model.RoomStatusInProgress {
		t.Fatalf("room view should not finalize ended active game, got %s", view.Status)
	}
}

func TestRoomViewIncludesCompletedGamesNewestFirst(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, rules.NewEngine())
	st.Put(&model.Game{
		ID:          "game-1",
		Status:      model.StatusEnded,
		RoundNumber: 4,
		EventSeq:    19,
		FinalScores: []model.Score{{PlayerID: 1, Wealth: 88, Rank: 2}},
	})
	st.Put(&model.Game{
		ID:          "game-3",
		Status:      model.StatusEnded,
		RoundNumber: 5,
		EventSeq:    41,
		FinalScores: []model.Score{{PlayerID: 2, Wealth: 130, Rank: 1}},
	})
	st.Put(&model.Game{
		ID:     "game-2",
		Status: model.StatusInProgress,
	})

	view, err := svc.LobbyView("")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.CompletedGames) != 2 {
		t.Fatalf("expected two completed games, got %+v", view.CompletedGames)
	}
	if view.CompletedGames[0].GameID != "game-3" || view.CompletedGames[1].GameID != "game-1" {
		t.Fatalf("expected newest completed games first, got %+v", view.CompletedGames)
	}
	if got := view.CompletedGames[0].Scores[0]; got.PlayerID != 2 || got.Wealth != 130 || got.Rank != 1 {
		t.Fatalf("expected completed scores to be included, got %+v", got)
	}
}

func TestCompletedGamesExcludeTutorialGames(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, rules.NewEngine())
	defaultRoomForTest(svc)

	tutorialGame, _, err := svc.CreateTutorial("overview")
	if err != nil {
		t.Fatal(err)
	}
	tutorialGame.Status = model.StatusEnded
	tutorialGame.RoundNumber = 5
	tutorialGame.FinalScores = []model.Score{{PlayerID: 1, Wealth: 155, Rank: 1}}

	regularGame := svc.CreateGame(nil)
	regularGame.Status = model.StatusEnded
	regularGame.RoundNumber = 4
	regularGame.FinalScores = []model.Score{{PlayerID: 2, Wealth: 130, Rank: 1}}

	lobby, err := svc.LobbyView("")
	if err != nil {
		t.Fatal(err)
	}
	if len(lobby.CompletedGames) != 1 || lobby.CompletedGames[0].GameID != regularGame.ID {
		t.Fatalf("expected only regular completed game in lobby, got %+v", lobby.CompletedGames)
	}

	room, err := svc.RoomViewInRoom(defaultRoomID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(room.CompletedGames) != 1 || room.CompletedGames[0].GameID != regularGame.ID {
		t.Fatalf("expected only regular completed game in room, got %+v", room.CompletedGames)
	}
}

func TestDisconnectInLobbyUnreadiesAndReleasesSeat(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	room := defaultRoomForTest(svc)

	svc.roomMu.Lock()
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Connections: 1, Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice", Ready: true}
	svc.roomMu.Unlock()

	svc.DisconnectRoomParticipant("alice")
	view, err := svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	seats := seatsByID(view.Seats)
	if seats[1].Type != model.SeatTypeHuman || seats[1].Ready || seats[1].Online {
		t.Fatalf("expected disconnected player to remain seated, offline, and unready, got %+v", seats[1])
	}

	svc.roomMu.Lock()
	svc.stopOfflineReleaseLocked(room, "alice")
	svc.roomMu.Unlock()
	svc.releaseOfflineSeat(defaultRoomID, "alice")
	view, err = svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	seats = seatsByID(view.Seats)
	if seats[1].Type != model.SeatTypeEmpty {
		t.Fatalf("expected offline seat to be released, got %+v", seats[1])
	}
}

func TestOfflineClaimSchedulesSeatRelease(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	room := defaultRoomForTest(svc)

	svc.roomMu.Lock()
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice"}
	svc.roomMu.Unlock()

	if err := svc.ClaimSeat("alice", 1); err != nil {
		t.Fatal(err)
	}
	view, err := svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	seats := seatsByID(view.Seats)
	if seats[1].Type != model.SeatTypeHuman || seats[1].Online {
		t.Fatalf("expected offline token to claim an offline seat, got %+v", seats[1])
	}

	svc.roomMu.Lock()
	svc.stopOfflineReleaseLocked(room, "alice")
	svc.roomMu.Unlock()
	svc.releaseOfflineSeat(defaultRoomID, "alice")
	view, err = svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	seats = seatsByID(view.Seats)
	if seats[1].Type != model.SeatTypeEmpty {
		t.Fatalf("expected offline claimed seat to release, got %+v", seats[1])
	}
}

func TestJoinedUnseatedParticipantCanManageAI(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	room := defaultRoomForTest(svc)

	svc.roomMu.Lock()
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	svc.roomMu.Unlock()

	if err := svc.PlaceAI("alice", 2); err != nil {
		t.Fatal(err)
	}
	view, err := svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	if !view.CanManageAI {
		t.Fatalf("expected joined unseated participant to manage AI, got %+v", view)
	}
	seats := seatsByID(view.Seats)
	if seats[2].Type != model.SeatTypeAI {
		t.Fatalf("expected P2 to contain AI, got %+v", seats[2])
	}
	if err := svc.RemoveAI("alice", 2); err != nil {
		t.Fatal(err)
	}
	view, err = svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	seats = seatsByID(view.Seats)
	if seats[2].Type != model.SeatTypeEmpty {
		t.Fatalf("expected P2 AI to be removed, got %+v", seats[2])
	}
}

func TestReadyParticipantCannotManageAI(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	room := defaultRoomForTest(svc)

	svc.roomMu.Lock()
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice", Ready: true}
	svc.roomMu.Unlock()

	if err := svc.PlaceAI("alice", 2); err == nil {
		t.Fatal("expected ready participant not to place AI")
	}
	view, err := svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.CanManageAI {
		t.Fatalf("expected ready participant not to manage AI, got %+v", view)
	}
}

func TestAllAISeatsDoNotStartRoomGame(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	room := defaultRoomForTest(svc)

	svc.roomMu.Lock()
	room.Participants["host"] = &roomParticipant{Token: "host", Name: "Host", Online: true}
	svc.roomMu.Unlock()

	for _, playerID := range model.PlayerOrder {
		if err := svc.PlaceAI("host", playerID); err != nil {
			t.Fatal(err)
		}
	}
	view, err := svc.RoomView("host")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != model.RoomStatusWaiting || view.GameID != "" {
		t.Fatalf("all-AI room should not start automatically, got %+v", view)
	}
}

func TestReviewTimeoutConfirmsUnconfirmedHumans(t *testing.T) {
	if operationTimeout != 60*time.Second {
		t.Fatalf("operation timeout = %s, want 60s", operationTimeout)
	}
	if offlineSeatTimeout != 60*time.Second {
		t.Fatalf("offline seat timeout = %s, want 60s", offlineSeatTimeout)
	}
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	room := defaultRoomForTest(svc)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.Round.ConfirmedPlayers = map[int]bool{1: true}
	st.Put(g)

	svc.roomMu.Lock()
	room.Status = model.RoomStatusInProgress
	room.ActiveGameID = "game-1"
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.scheduleReviewTimeoutLocked(room, g)
	if room.TimeoutKind != roomTimeoutReview {
		t.Fatalf("expected review timeout to be scheduled, got %q", room.TimeoutKind)
	}
	remaining := time.Until(room.TimeoutDeadline)
	if remaining <= 59*time.Second || remaining > 60*time.Second {
		t.Fatalf("expected roughly 60s review timeout, got %s", remaining)
	}
	svc.stopTimeoutLocked(room)
	svc.roomMu.Unlock()

	svc.applyReviewTimeout(defaultRoomID, g.EventSeq)
	if !g.Round.ConfirmedPlayers[2] {
		t.Fatalf("expected unconfirmed player to be confirmed, got %+v", g.Round.ConfirmedPlayers)
	}
}

func TestReviewTimeoutSkipsSingleHumanGame(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	room := defaultRoomForTest(svc)
	g := &model.Game{
		EventSeq: 7,
		Round:    model.RoundState{ConfirmedPlayers: map[int]bool{}},
	}

	svc.roomMu.Lock()
	room.Status = model.RoomStatusInProgress
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	room.Seats[2] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.scheduleReviewTimeoutLocked(room, g)
	if room.TimeoutKind != "" {
		t.Fatalf("expected single-human review to have no timeout, got %q", room.TimeoutKind)
	}
	svc.roomMu.Unlock()
}

func TestSchedulingHumanTimeoutBroadcastsGameChange(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	room := defaultRoomForTest(svc)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	st.Put(g)

	svc.roomMu.Lock()
	room.Status = model.RoomStatusInProgress
	room.ActiveGameID = "game-1"
	room.RoundNumber = g.RoundNumber
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.roomMu.Unlock()

	gameID, changed, roomChanged, continueLoop := svc.processRoomAutomationStep(defaultRoomID)
	if gameID != "game-1" || !changed || roomChanged || continueLoop {
		t.Fatalf("expected timeout scheduling to request game broadcast, got gameID=%q changed=%v roomChanged=%v continue=%v", gameID, changed, roomChanged, continueLoop)
	}
	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	defer svc.stopTimeoutLocked(room)
	if room.TimeoutKind != roomTimeoutAction {
		t.Fatalf("expected action timeout to be scheduled, got %q", room.TimeoutKind)
	}
}

func TestRoundReviewClearsTemporaryAITakeover(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	room := defaultRoomForTest(svc)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.Round.ConfirmedPlayers = map[int]bool{}
	st.Put(g)

	svc.roomMu.Lock()
	room.Status = model.RoomStatusInProgress
	room.ActiveGameID = "game-1"
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.TempAI = map[int]bool{1: true}
	svc.roomMu.Unlock()

	svc.processRoomAutomationStep(defaultRoomID)
	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	svc.stopTimeoutLocked(room)
	if len(room.TempAI) != 0 {
		t.Fatalf("expected temporary AI takeover to stop at review, got %+v", room.TempAI)
	}
}

func TestAIRoundFromReviewTakesOverNextRound(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	room := defaultRoomForTest(svc)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.PreviousHarborMaster = 2
	g.Round.ConfirmedPlayers = map[int]bool{2: true, 3: true, 4: true}
	st.Put(g)

	svc.roomMu.Lock()
	room.Status = model.RoomStatusInProgress
	room.ActiveGameID = "game-1"
	room.RoundNumber = g.RoundNumber
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.roomMu.Unlock()

	if _, err := svc.RoomAIRound("alice"); err != nil {
		t.Fatal(err)
	}

	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	svc.stopTimeoutLocked(room)
	if g.Phase == model.PhaseRoundReview {
		t.Fatal("expected AI round confirm to advance to the next round")
	}
	if !room.TempAI[1] {
		t.Fatalf("expected player 1 to be taken over after review, got temp=%+v pending=%+v", room.TempAI, room.PendingAI)
	}
	if len(room.PendingAI) != 0 {
		t.Fatalf("expected pending takeover to be activated, got %+v", room.PendingAI)
	}
}

func TestAIStepFromReviewOnlyConfirms(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	room := defaultRoomForTest(svc)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.Round.ConfirmedPlayers = map[int]bool{}
	st.Put(g)

	svc.roomMu.Lock()
	room.Status = model.RoomStatusInProgress
	room.ActiveGameID = "game-1"
	room.RoundNumber = g.RoundNumber
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.roomMu.Unlock()

	_, action, err := svc.RoomAIStep("alice")
	if err != nil {
		t.Fatal(err)
	}
	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	svc.stopTimeoutLocked(room)
	if action.Type != model.ActionConfirmRound || !g.Round.ConfirmedPlayers[1] {
		t.Fatalf("expected AI step in review to confirm only, action=%+v confirmed=%+v", action, g.Round.ConfirmedPlayers)
	}
	if len(room.TempAI) != 0 || len(room.PendingAI) != 0 {
		t.Fatalf("expected AI step in review not to schedule takeover, temp=%+v pending=%+v", room.TempAI, room.PendingAI)
	}
}

func TestManualAIActionCountsAsHumanAction(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	room := defaultRoomForTest(svc)

	svc.roomMu.Lock()
	room.HumanActionThisRound = false
	svc.roomMu.Unlock()

	svc.markHumanAction(defaultRoomID)

	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	if !room.HumanActionThisRound {
		t.Fatal("expected manually requested AI action to count as human activity")
	}
}

func TestManualConfirmCountsAsHumanAction(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	room := defaultRoomForTest(svc)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.Round.ConfirmedPlayers = map[int]bool{}
	st.Put(g)

	svc.roomMu.Lock()
	room.Status = model.RoomStatusInProgress
	room.ActiveGameID = "game-1"
	room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.roomMu.Unlock()

	if _, err := svc.ApplyRoomAction("alice", model.Action{PlayerID: 1, Type: model.ActionConfirmRound}); err != nil {
		t.Fatal(err)
	}
	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	svc.stopTimeoutLocked(room)
	if !room.HumanActionThisRound {
		t.Fatal("expected manual confirm to count as human activity")
	}
}

func seatsByID(seats []model.RoomSeat) map[int]model.RoomSeat {
	byID := map[int]model.RoomSeat{}
	for _, seat := range seats {
		byID[seat.PlayerID] = seat
	}
	return byID
}
