package app

import (
	"testing"

	"manila/internal/model"
	"manila/internal/rules"
	"manila/internal/store"
)

func TestFinishEndedRoomGameReturnsToLobbyWithHumanSeats(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())

	scores := []model.Score{
		{PlayerID: 1, Wealth: 128, Rank: 1},
		{PlayerID: 2, Wealth: 96, Rank: 2},
		{PlayerID: 3, Wealth: 72, Rank: 3},
		{PlayerID: 4, Wealth: 64, Rank: 4},
	}

	svc.roomMu.Lock()
	svc.room.Status = model.RoomStatusInProgress
	svc.room.ActiveGameID = "game-1"
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	svc.room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	svc.room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice", Ready: true}
	svc.room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob", Ready: true}
	svc.room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.TempAI = map[int]bool{1: true}
	svc.finishEndedRoomGameLocked(&model.Game{ID: "game-1", EventSeq: 42, FinalScores: scores})
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
	if !view.Participant.Joined || view.Participant.PlayerID != 1 {
		t.Fatalf("expected Alice to remain seated, got %+v", view.Participant)
	}
	if !view.CanReady || !view.CanLeaveSeat || !view.CanManageAI {
		t.Fatalf("expected Alice to be unready and able to manage lobby, got %+v", view)
	}
	if view.LastSettlement == nil {
		t.Fatal("expected settlement snapshot")
	}
	if view.LastSettlement.GameID != "game-1" || view.LastSettlement.EventSeq != 42 || len(view.LastSettlement.Scores) != 4 {
		t.Fatalf("unexpected settlement snapshot: %+v", view.LastSettlement)
	}

	seats := seatsByID(view.Seats)
	if seats[1].Type != model.SeatTypeHuman || seats[1].Ready {
		t.Fatalf("expected P1 human seat to remain unready, got %+v", seats[1])
	}
	if seats[2].Type != model.SeatTypeHuman || seats[2].Ready {
		t.Fatalf("expected P2 human seat to remain unready, got %+v", seats[2])
	}
	if seats[3].Type != model.SeatTypeEmpty || seats[4].Type != model.SeatTypeEmpty {
		t.Fatalf("expected AI seats to be cleared, got P3=%+v P4=%+v", seats[3], seats[4])
	}
}

func TestRoomViewFinalizesEndedActiveGame(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, rules.NewEngine())
	g := &model.Game{
		ID:          "game-1",
		Status:      model.StatusEnded,
		EventSeq:    9,
		FinalScores: []model.Score{{PlayerID: 1, Wealth: 120, Rank: 1}},
	}
	st.Put(g)

	svc.roomMu.Lock()
	svc.room.Status = model.RoomStatusInProgress
	svc.room.ActiveGameID = "game-1"
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	svc.room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice", Ready: true}
	svc.room.Seats[2] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.roomMu.Unlock()

	view, err := svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != model.RoomStatusWaiting {
		t.Fatalf("expected ended active game to return room to lobby, got %s", view.Status)
	}
	if view.LastSettlement == nil || view.LastSettlement.GameID != "game-1" || len(view.LastSettlement.Scores) != 1 {
		t.Fatalf("expected settlement snapshot from ended active game, got %+v", view.LastSettlement)
	}
}

func TestDisconnectInLobbyUnreadiesAndReleasesSeat(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())

	svc.roomMu.Lock()
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Connections: 1, Online: true}
	svc.room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice", Ready: true}
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
	svc.stopOfflineReleaseLocked("alice")
	svc.roomMu.Unlock()
	svc.releaseOfflineSeat("alice")
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

	svc.roomMu.Lock()
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice"}
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
	svc.stopOfflineReleaseLocked("alice")
	svc.roomMu.Unlock()
	svc.releaseOfflineSeat("alice")
	view, err = svc.RoomView("alice")
	if err != nil {
		t.Fatal(err)
	}
	seats = seatsByID(view.Seats)
	if seats[1].Type != model.SeatTypeEmpty {
		t.Fatalf("expected offline claimed seat to release, got %+v", seats[1])
	}
}

func TestReviewTimeoutConfirmsUnconfirmedHumans(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.Round.ConfirmedPlayers = map[int]bool{1: true}
	st.Put(g)

	svc.roomMu.Lock()
	svc.room.Status = model.RoomStatusInProgress
	svc.room.ActiveGameID = "game-1"
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	svc.room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	svc.room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	svc.room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	svc.room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.scheduleReviewTimeoutLocked(g)
	if svc.room.TimeoutKind != roomTimeoutReview {
		t.Fatalf("expected review timeout to be scheduled, got %q", svc.room.TimeoutKind)
	}
	svc.stopTimeoutLocked()
	svc.roomMu.Unlock()

	svc.applyReviewTimeout(g.EventSeq)
	if !g.Round.ConfirmedPlayers[2] {
		t.Fatalf("expected unconfirmed player to be confirmed, got %+v", g.Round.ConfirmedPlayers)
	}
}

func TestReviewTimeoutSkipsSingleHumanGame(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	g := &model.Game{
		EventSeq: 7,
		Round:    model.RoundState{ConfirmedPlayers: map[int]bool{}},
	}

	svc.roomMu.Lock()
	svc.room.Status = model.RoomStatusInProgress
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	svc.room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	svc.room.Seats[2] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.scheduleReviewTimeoutLocked(g)
	if svc.room.TimeoutKind != "" {
		t.Fatalf("expected single-human review to have no timeout, got %q", svc.room.TimeoutKind)
	}
	svc.roomMu.Unlock()
}

func TestRoundReviewClearsTemporaryAITakeover(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.Round.ConfirmedPlayers = map[int]bool{}
	st.Put(g)

	svc.roomMu.Lock()
	svc.room.Status = model.RoomStatusInProgress
	svc.room.ActiveGameID = "game-1"
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	svc.room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	svc.room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	svc.room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	svc.room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.TempAI = map[int]bool{1: true}
	svc.roomMu.Unlock()

	svc.processRoomAutomationStep()
	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	svc.stopTimeoutLocked()
	if len(svc.room.TempAI) != 0 {
		t.Fatalf("expected temporary AI takeover to stop at review, got %+v", svc.room.TempAI)
	}
}

func TestAIRoundFromReviewTakesOverNextRound(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
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
	svc.room.Status = model.RoomStatusInProgress
	svc.room.ActiveGameID = "game-1"
	svc.room.RoundNumber = g.RoundNumber
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	svc.room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	svc.room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	svc.room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	svc.room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.roomMu.Unlock()

	if _, err := svc.RoomAIRound("alice"); err != nil {
		t.Fatal(err)
	}

	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	svc.stopTimeoutLocked()
	if g.Phase == model.PhaseRoundReview {
		t.Fatal("expected AI round confirm to advance to the next round")
	}
	if !svc.room.TempAI[1] {
		t.Fatalf("expected player 1 to be taken over after review, got temp=%+v pending=%+v", svc.room.TempAI, svc.room.PendingAI)
	}
	if len(svc.room.PendingAI) != 0 {
		t.Fatalf("expected pending takeover to be activated, got %+v", svc.room.PendingAI)
	}
}

func TestAIStepFromReviewOnlyConfirms(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.Round.ConfirmedPlayers = map[int]bool{}
	st.Put(g)

	svc.roomMu.Lock()
	svc.room.Status = model.RoomStatusInProgress
	svc.room.ActiveGameID = "game-1"
	svc.room.RoundNumber = g.RoundNumber
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	svc.room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	svc.room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	svc.room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	svc.room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.roomMu.Unlock()

	_, action, err := svc.RoomAIStep("alice")
	if err != nil {
		t.Fatal(err)
	}
	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	svc.stopTimeoutLocked()
	if action.Type != model.ActionConfirmRound || !g.Round.ConfirmedPlayers[1] {
		t.Fatalf("expected AI step in review to confirm only, action=%+v confirmed=%+v", action, g.Round.ConfirmedPlayers)
	}
	if len(svc.room.TempAI) != 0 || len(svc.room.PendingAI) != 0 {
		t.Fatalf("expected AI step in review not to schedule takeover, temp=%+v pending=%+v", svc.room.TempAI, svc.room.PendingAI)
	}
}

func TestManualAIActionCountsAsHumanAction(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())

	svc.roomMu.Lock()
	svc.room.HumanActionThisRound = false
	svc.roomMu.Unlock()

	svc.markHumanAction()

	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	if !svc.room.HumanActionThisRound {
		t.Fatal("expected manually requested AI action to count as human activity")
	}
}

func TestManualConfirmCountsAsHumanAction(t *testing.T) {
	st := store.NewMemoryStore()
	eng := rules.NewEngine()
	svc := NewService(st, eng)
	g := rules.NewGame("game-1", 1)
	if err := eng.StartGame(g); err != nil {
		t.Fatal(err)
	}
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.Round.ConfirmedPlayers = map[int]bool{}
	st.Put(g)

	svc.roomMu.Lock()
	svc.room.Status = model.RoomStatusInProgress
	svc.room.ActiveGameID = "game-1"
	svc.room.Participants["alice"] = &roomParticipant{Token: "alice", Name: "Alice", Online: true}
	svc.room.Participants["bob"] = &roomParticipant{Token: "bob", Name: "Bob", Online: true}
	svc.room.Seats[1] = &roomSeat{Type: model.SeatTypeHuman, Token: "alice"}
	svc.room.Seats[2] = &roomSeat{Type: model.SeatTypeHuman, Token: "bob"}
	svc.room.Seats[3] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.room.Seats[4] = &roomSeat{Type: model.SeatTypeAI, Ready: true}
	svc.roomMu.Unlock()

	if _, err := svc.ApplyRoomAction("alice", model.Action{PlayerID: 1, Type: model.ActionConfirmRound}); err != nil {
		t.Fatal(err)
	}
	svc.roomMu.Lock()
	defer svc.roomMu.Unlock()
	svc.stopTimeoutLocked()
	if !svc.room.HumanActionThisRound {
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
