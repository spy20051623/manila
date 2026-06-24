package model

type GameStatus string

const (
	StatusNotStarted GameStatus = "notStarted"
	StatusInProgress GameStatus = "inProgress"
	StatusEnded      GameStatus = "ended"
)

type Phase string

const (
	PhaseNotStarted              Phase = "NotStarted"
	PhaseAuction                 Phase = "Auction"
	PhaseHarborMasterBuyShare    Phase = "HarborMasterBuyShare"
	PhaseHarborMasterSelectGoods Phase = "HarborMasterSelectGoods"
	PhaseHarborMasterSetShips    Phase = "HarborMasterSetShips"
	PhasePlacement               Phase = "Placement"
	PhasePirateBoarding          Phase = "PirateBoarding"
	PhaseNavigatorAction         Phase = "NavigatorAction"
	PhasePirateLooting           Phase = "PirateLooting"
	PhaseRoundReview             Phase = "RoundReview"
	PhaseGameEnd                 Phase = "GameEnd"
)

type ActionType string

const (
	ActionBid                     ActionType = "Bid"
	ActionPassBid                 ActionType = "PassBid"
	ActionBuyShare                ActionType = "BuyShare"
	ActionSkipBuyShare            ActionType = "SkipBuyShare"
	ActionSelectGoods             ActionType = "SelectGoods"
	ActionSetShipStarts           ActionType = "SetShipStarts"
	ActionPlaceAccomplice         ActionType = "PlaceAccomplice"
	ActionPirateBoard             ActionType = "PirateBoard"
	ActionPirateSkipBoard         ActionType = "PirateSkipBoard"
	ActionNavigatorMove           ActionType = "NavigatorMove"
	ActionNavigatorSkip           ActionType = "NavigatorSkip"
	ActionPirateChooseDestination ActionType = "PirateChooseDestination"
	ActionConfirmRound            ActionType = "ConfirmRound"
)

type GoodsID int

const (
	GoodsGinseng GoodsID = 1
	GoodsNutmeg  GoodsID = 2
	GoodsSilk    GoodsID = 3
	GoodsJade    GoodsID = 4
)

var GoodsOrder = []GoodsID{GoodsGinseng, GoodsNutmeg, GoodsSilk, GoodsJade}
var PlayerOrder = []int{1, 2, 3, 4}
var MarketValues = []int{0, 5, 10, 20, 30}

func GoodsName(id GoodsID) string {
	switch id {
	case GoodsGinseng:
		return "ginseng"
	case GoodsNutmeg:
		return "nutmeg"
	case GoodsSilk:
		return "silk"
	case GoodsJade:
		return "jade"
	default:
		return "unknown"
	}
}

func ShipPayout(id GoodsID) int {
	switch id {
	case GoodsGinseng:
		return 18
	case GoodsNutmeg:
		return 24
	case GoodsSilk:
		return 30
	case GoodsJade:
		return 36
	default:
		return 0
	}
}

func BoardingCosts(id GoodsID) []int {
	switch id {
	case GoodsGinseng:
		return []int{1, 2, 3}
	case GoodsNutmeg:
		return []int{2, 3, 4}
	case GoodsSilk:
		return []int{3, 4, 5}
	case GoodsJade:
		return []int{3, 4, 5, 5}
	default:
		return nil
	}
}

type Game struct {
	ID                   string             `json:"gameId"`
	Status               GameStatus         `json:"status"`
	Phase                Phase              `json:"phase"`
	RoundNumber          int                `json:"roundNumber"`
	Players              map[int]*Player    `json:"players"`
	Goods                map[GoodsID]*Goods `json:"goods"`
	Ships                map[GoodsID]*Ship  `json:"ships"`
	Board                Board              `json:"board"`
	Auction              Auction            `json:"auction"`
	Round                RoundState         `json:"round"`
	CurrentPlayer        int                `json:"currentPlayer"`
	HarborMaster         int                `json:"harborMaster"`
	PreviousHarborMaster int                `json:"previousHarborMaster"`
	Seed                 int64              `json:"seed,omitempty"`
	RandomDraws          int                `json:"randomDraws"`
	EventSeq             int                `json:"eventSeq"`
	Events               []Event            `json:"events"`
	FinalScores          []Score            `json:"finalScores,omitempty"`
}

type Player struct {
	ID                  int             `json:"playerId"`
	Cash                int             `json:"cash"`
	Shares              map[GoodsID]int `json:"shares"`
	HiddenShareCount    int             `json:"hiddenShareCount,omitempty"`
	MortgagedShareCount int             `json:"mortgagedShareCount"`
	AvailablePieces     int             `json:"availableAccomplices"`
	PlacedPieces        int             `json:"placedAccomplices"`
	BankruptThisRound   bool            `json:"bankruptThisRound"`
}

type Goods struct {
	ID               GoodsID `json:"goodsId"`
	Name             string  `json:"name"`
	MarketValueIndex int     `json:"marketValueIndex"`
	SharesRemaining  int     `json:"sharesRemaining"`
	ShipPayout       int     `json:"shipPayout"`
	BoardingCosts    []int   `json:"boardingCosts"`
}

func (g Goods) MarketValue() int {
	if g.MarketValueIndex < 0 || g.MarketValueIndex >= len(MarketValues) {
		return 0
	}
	return MarketValues[g.MarketValueIndex]
}

type ShipStatus string

const (
	ShipSailing ShipStatus = "sailing"
	ShipArrived ShipStatus = "arrived"
	ShipDocked  ShipStatus = "docked"
)

type Ship struct {
	ID                GoodsID    `json:"shipId"`
	GoodsID           GoodsID    `json:"goodsId"`
	RouteIndex        int        `json:"routeIndex"`
	Position          int        `json:"position"`
	Status            ShipStatus `json:"status"`
	Occupants         []Piece    `json:"occupants"`
	Stowaways         []Piece    `json:"stowaways"`
	PortSlot          string     `json:"portSlot,omitempty"`
	DockSlot          string     `json:"dockSlot,omitempty"`
	LootedByPirates   bool       `json:"lootedByPirates"`
	NextBoardingIndex int        `json:"nextBoardingSlot"`
}

type Piece struct {
	PlayerID int    `json:"playerId"`
	Role     string `json:"role,omitempty"`
}

type Board struct {
	Ports          map[string]*BoardSlot `json:"ports"`
	Docks          map[string]*BoardSlot `json:"docks"`
	Pirates        []Piece               `json:"pirates"`
	BoardedPirates []Piece               `json:"boardedPirates,omitempty"`
	SmallNavigator *Piece                `json:"smallNavigator,omitempty"`
	BigNavigator   *Piece                `json:"bigNavigator,omitempty"`
	Insurance      *Piece                `json:"insurance,omitempty"`
}

type BoardSlot struct {
	ID       string `json:"id"`
	Cost     int    `json:"cost"`
	Reward   int    `json:"reward"`
	Occupant *Piece `json:"occupant,omitempty"`
}

type Auction struct {
	StartPlayer   int          `json:"startPlayer"`
	ActivePlayer  int          `json:"activePlayer"`
	CurrentBid    int          `json:"currentBid"`
	HighestBidder int          `json:"highestBidder"`
	PassedPlayers map[int]bool `json:"passedPlayers"`
	HasAnyBid     bool         `json:"hasAnyBid"`
}

type RoundState struct {
	SelectedGoods       []GoodsID    `json:"selectedGoods"`
	ExcludedGoods       GoodsID      `json:"excludedGoods,omitempty"`
	PlacementStep       int          `json:"placementStep"`
	MovementStep        int          `json:"movementStep"`
	PlacementTurnsTaken int          `json:"placementTurnsTaken"`
	DiceResults         []DiceRoll   `json:"diceResults"`
	PendingPirateQueue  []Piece      `json:"pendingPirateQueue,omitempty"`
	PendingLootShips    []GoodsID    `json:"pendingLootShips,omitempty"`
	NavigatorStep       string       `json:"navigatorStep,omitempty"`
	ArrivedOrder        []GoodsID    `json:"arrivedOrder"`
	DockedOrder         []GoodsID    `json:"dockedOrder"`
	ConfirmedPlayers    map[int]bool `json:"confirmedPlayers,omitempty"`
}

type DiceRoll struct {
	MovementStep int             `json:"movementStep"`
	Results      map[GoodsID]int `json:"results"`
}

type Action struct {
	ActionID         string                 `json:"actionId,omitempty"`
	GameID           string                 `json:"gameId,omitempty"`
	PlayerID         int                    `json:"playerId"`
	Type             ActionType             `json:"type"`
	ExpectedEventSeq *int                   `json:"expectedEventSeq,omitempty"`
	Payload          map[string]interface{} `json:"payload,omitempty"`
}

type LegalAction struct {
	Type        ActionType             `json:"type"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	Cost        int                    `json:"cost,omitempty"`
	Description string                 `json:"description,omitempty"`
}

type Event struct {
	Seq     int                    `json:"seq"`
	Type    string                 `json:"type"`
	Message string                 `json:"message,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

type Score struct {
	PlayerID int `json:"playerId"`
	Wealth   int `json:"wealth"`
	Rank     int `json:"rank"`
}

type CompletedGameSummary struct {
	GameID      string  `json:"gameId"`
	RoundNumber int     `json:"roundNumber"`
	EventSeq    int     `json:"eventSeq"`
	Scores      []Score `json:"scores"`
}

type RoomStatus string

const (
	RoomStatusWaiting    RoomStatus = "waiting"
	RoomStatusInProgress RoomStatus = "inProgress"
	RoomStatusClosing    RoomStatus = "closing"
)

type SeatType string

const (
	SeatTypeEmpty SeatType = "empty"
	SeatTypeHuman SeatType = "human"
	SeatTypeAI    SeatType = "ai"
)

type RoomSeat struct {
	PlayerID int      `json:"playerId"`
	Type     SeatType `json:"type"`
	Name     string   `json:"name,omitempty"`
	Ready    bool     `json:"ready"`
	Online   bool     `json:"online"`
	IsYou    bool     `json:"isYou,omitempty"`
}

type RoomParticipant struct {
	Joined   bool   `json:"joined"`
	PlayerID int    `json:"playerId,omitempty"`
	Name     string `json:"name,omitempty"`
}

type RoomView struct {
	Status           RoomStatus             `json:"status"`
	Seats            []RoomSeat             `json:"seats"`
	Participant      RoomParticipant        `json:"participant"`
	GameID           string                 `json:"gameId,omitempty"`
	Game             *Game                  `json:"game,omitempty"`
	EventSeq         int                    `json:"eventSeq"`
	ClosingSeconds   int                    `json:"closingSeconds,omitempty"`
	CloseReason      string                 `json:"closeReason,omitempty"`
	SuggestedName    string                 `json:"suggestedName,omitempty"`
	CanManageAI      bool                   `json:"canManageAI"`
	CanReady         bool                   `json:"canReady"`
	CanCancelReady   bool                   `json:"canCancelReady"`
	CanLeaveSeat     bool                   `json:"canLeaveSeat"`
	CanAIForCurrent  bool                   `json:"canAIForCurrent"`
	HumanPlayerCount int                    `json:"humanPlayerCount"`
	CompletedGames   []CompletedGameSummary `json:"completedGames,omitempty"`
}
