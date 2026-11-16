package inspections

// Os tipos de vistoria.
const (
	KindEntry       = "entry"       // adesão: antes de o veículo entrar na proteção
	KindPeriodic    = "periodic"    // revistoria
	KindClaim       = "claim"       // sinistro: a avaria
	KindReplacement = "replacement" // troca de veículo
)

// KindLabels: como a tela escreve cada tipo.
var KindLabels = map[string]string{
	KindEntry: "Adesão", KindPeriodic: "Revistoria", KindClaim: "Sinistro", KindReplacement: "Troca de veículo",
}

// Quem tira as fotos.
const (
	ModeSelf       = "self"       // o associado (ou quem vai entrar), pelo app ou pelo link
	ModeTechnician = "technician" // um técnico, no lugar dele
)

// ModeLabels: como a fatura e a tela escrevem quem fez.
var ModeLabels = map[string]string{ModeSelf: "Autovistoria", ModeTechnician: "Vistoria com técnico"}

// Os veículos (o roteiro muda: a moto não tem capô, estepe nem banco de trás).
const (
	VehicleCar        = "car"
	VehicleMotorcycle = "motorcycle"
)

// Slot é um ângulo do roteiro: o que a tela pede e como tirar.
type Slot struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Hint     string `json:"hint"`
	Optional bool   `json:"optional,omitempty"`
}

// catalog são os ângulos; a moto tem a dica dela onde muda.
var catalog = map[string]struct {
	Slot
	motoHint string
}{
	"front":       {Slot{ID: "front", Label: "Frente", Hint: "O veículo inteiro, de frente, com a placa legível"}, ""},
	"front_left":  {Slot{ID: "front_left", Label: "Frente e lado esquerdo", Hint: "Na diagonal, pegando a frente e a lateral do motorista"}, ""},
	"left":        {Slot{ID: "left", Label: "Lateral esquerda", Hint: "De lado, do para-choque da frente ao de trás"}, "De lado, da roda da frente à de trás"},
	"rear_left":   {Slot{ID: "rear_left", Label: "Traseira e lado esquerdo", Hint: "Na diagonal, pegando a traseira e a lateral do motorista"}, ""},
	"rear":        {Slot{ID: "rear", Label: "Traseira", Hint: "O veículo inteiro, de trás, com a placa legível"}, ""},
	"rear_right":  {Slot{ID: "rear_right", Label: "Traseira e lado direito", Hint: "Na diagonal, pegando a traseira e a lateral do passageiro"}, ""},
	"right":       {Slot{ID: "right", Label: "Lateral direita", Hint: "De lado, do para-choque da frente ao de trás"}, "De lado, da roda da frente à de trás"},
	"front_right": {Slot{ID: "front_right", Label: "Frente e lado direito", Hint: "Na diagonal, pegando a frente e a lateral do passageiro"}, ""},
	"dashboard": {Slot{ID: "dashboard", Label: "Painel (hodômetro)", Hint: "Com o veículo ligado, mostrando a quilometragem"},
		"Com a moto ligada, mostrando a quilometragem"},
	"chassis": {Slot{ID: "chassis", Label: "Número do chassi", Hint: "Gravado no vidro ou no assoalho, legível"},
		"Gravado no quadro, perto do guidão, legível"},
	"engine":   {Slot{ID: "engine", Label: "Motor", Hint: "Com o capô aberto, o motor inteiro"}, "O motor, pelo lado direito da moto"},
	"interior": {Slot{ID: "interior", Label: "Interior", Hint: "Os bancos e o painel, pela porta do motorista"}, ""},
	"wheel": {Slot{ID: "wheel", Label: "Pneus e rodas", Hint: "Uma roda da frente de perto, mostrando o desgaste do pneu"},
		"As duas rodas, mostrando o desgaste dos pneus"},
	"spare":         {Slot{ID: "spare", Label: "Estepe", Hint: "O estepe, o macaco e a chave de roda"}, ""},
	"document":      {Slot{ID: "document", Label: "Documento do veículo", Hint: "O CRLV inteiro, legível"}, ""},
	"damage":        {Slot{ID: "damage", Label: "A avaria", Hint: "A avaria de longe, mostrando onde fica no veículo"}, ""},
	"damage_detail": {Slot{ID: "damage_detail", Label: "A avaria de perto", Hint: "A avaria de perto, com boa luz"}, ""},
	"damage_extra": {Slot{ID: "damage_extra", Label: "Outra avaria", Hint: "Se houver outra avaria, a foto dela",
		Optional: true}, ""},
}

// routes: os ângulos de cada tipo, na ordem em que a pessoa anda em volta do veículo.
var routes = map[string]map[string][]string{
	KindPeriodic: {
		VehicleCar:        {"front", "rear", "left", "right", "dashboard", "chassis"},
		VehicleMotorcycle: {"front", "rear", "left", "right", "dashboard", "chassis"},
	},
	KindEntry: {
		VehicleCar: {"front", "front_left", "left", "rear_left", "rear", "rear_right", "right", "front_right",
			"dashboard", "chassis", "engine", "interior", "wheel", "spare", "document"},
		VehicleMotorcycle: {"front", "left", "rear", "right", "dashboard", "chassis", "engine", "wheel", "document"},
	},
	KindClaim: {
		VehicleCar:        {"front", "rear", "left", "right", "damage", "damage_detail", "damage_extra", "dashboard"},
		VehicleMotorcycle: {"front", "rear", "left", "right", "damage", "damage_detail", "damage_extra", "dashboard"},
	},
}

func init() { routes[KindReplacement] = routes[KindEntry] }

// Route é o roteiro das fotos do tipo de vistoria, para o veículo.
func Route(kind, vehicleType string) []Slot {
	byType, ok := routes[kind]
	if !ok {
		byType = routes[KindPeriodic]
	}
	ids, ok := byType[vehicleType]
	if !ok {
		ids = byType[VehicleCar]
	}
	out := make([]Slot, len(ids))
	for i, id := range ids {
		c := catalog[id]
		out[i] = c.Slot
		if vehicleType == VehicleMotorcycle && c.motoHint != "" {
			out[i].Hint = c.motoHint
		}
	}
	return out
}

// validKind e validVehicle: os valores aceitos no pedido.
func validKind(kind string) bool { _, ok := KindLabels[kind]; return ok }

func validVehicle(t string) bool { return t == VehicleCar || t == VehicleMotorcycle }
