package vehicles

// MaxPlanCents: a mensalidade de um tipo de veículo vai até R$ 100.000.
const MaxPlanCents = 10_000_000

// KindPrices é a mensalidade do plano por tipo de veículo (a tabela do link
// de um afiliado): a moto, o carro e o caminhão, cada um com o seu preço.
type KindPrices struct {
	MotorcycleCents int `json:"motorcycleCents"`
	CarCents        int `json:"carCents"`
	TruckCents      int `json:"truckCents"`
}

// For é a mensalidade do tipo (sem tipo, a do carro, como no cadastro).
func (p KindPrices) For(kind string) int {
	switch normalizeKind(kind) {
	case KindMotorcycle:
		return p.MotorcycleCents
	case KindTruck:
		return p.TruckCents
	default:
		return p.CarCents
	}
}

// Valid: os três preços preenchidos, dentro da faixa.
func (p KindPrices) Valid() bool {
	for _, cents := range []int{p.MotorcycleCents, p.CarCents, p.TruckCents} {
		if cents <= 0 || cents > MaxPlanCents {
			return false
		}
	}
	return true
}
