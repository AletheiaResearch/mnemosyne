package review

type Screen struct {
	Title string
	Body  string
}

func New(title, body string) Screen {
	return Screen{Title: title, Body: body}
}
