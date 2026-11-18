package audio

type Device struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}
