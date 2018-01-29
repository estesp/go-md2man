package md2man

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/russross/blackfriday"
)

type roffRenderer struct {
	extensions   blackfriday.Extensions
	ListCounters []int
	firstHeader  bool
	defineTerm   bool
	inList       bool
}

const (
	titleHeader      = ".TH "
	topLevelHeader   = "\n\n.SH "
	secondLevelHdr   = "\n.SH "
	otherHeader      = "\n.SS "
	crTag            = "\n"
	emphTag          = "\\fI"
	emphCloseTag     = "\\fP"
	strongTag        = "\\fB"
	strongCloseTag   = "\\fP"
	breakTag         = "\n.br\n"
	paraTag          = "\n.PP\n"
	hruleTag         = "\n.ti 0\n\\l'\\n(.lu'\n"
	linkTag          = "\n\\[la]"
	linkCloseTag     = "\\[ra]"
	codespanTag      = "\\fB\\fC"
	codespanCloseTag = "\\fR"
	codeTag          = "\n.PP\n.RS\n\n.nf\n"
	codeCloseTag     = "\n.fi\n.RE\n"
	quoteTag         = "\n.PP\n.RS\n"
	quoteCloseTag    = "\n.RE\n"
	listTag          = "\n.RS\n"
	listCloseTag     = "\n.RE\n"
	arglistTag       = "\n.TP\n"
	tableStart       = "\n.TS\nallbox;\n"
	tableEnd         = "\n.TE\n"
	tableCellStart   = "\nT{\n"
	tableCellEnd     = "\nT}\n"
)

// NewRoffRenderer creates a new blackfriday Renderer for generating roff documents
// from markdown
func NewRoffRenderer() *roffRenderer {
	var extensions blackfriday.Extensions

	extensions |= blackfriday.NoIntraEmphasis
	extensions |= blackfriday.Tables
	extensions |= blackfriday.FencedCode
	extensions |= blackfriday.SpaceHeadings
	extensions |= blackfriday.Footnotes
	extensions |= blackfriday.Titleblock
	extensions |= blackfriday.DefinitionLists
	return &roffRenderer{
		extensions: extensions,
	}
}

func (r *roffRenderer) GetExtensions() blackfriday.Extensions {
	return r.extensions
}

func (r *roffRenderer) RenderHeader(w io.Writer, ast *blackfriday.Node) {
	// disable hyphenation
	io.WriteString(w, ".nh\n")
	return
}

func (r *roffRenderer) RenderFooter(w io.Writer, ast *blackfriday.Node) {
	return
}

func (r *roffRenderer) RenderNode(w io.Writer, node *blackfriday.Node, entering bool) blackfriday.WalkStatus {

	switch node.Type {
	case blackfriday.Text:
		var (
			start, end string
		)
		if node.Parent.Type == blackfriday.TableCell {
			if len(node.Literal) > 30 {
				start = tableCellStart
				end = tableCellEnd
			}
		}
		out(w, start)
		escapeSpecialChars(w, node.Literal)
		out(w, end)
	case blackfriday.Softbreak:
		out(w, crTag)
	case blackfriday.Hardbreak:
		out(w, breakTag)
	case blackfriday.Emph:
		if entering {
			out(w, emphTag)
		} else {
			out(w, emphCloseTag)
		}
	case blackfriday.Strong:
		if entering {
			out(w, strongTag)
		} else {
			out(w, strongCloseTag)
		}
	case blackfriday.Link:
		if entering {
			out(w, linkTag+string(node.LinkData.Destination))
		} else {
			out(w, linkCloseTag)
		}
	case blackfriday.Image:
		// ignore images
		return blackfriday.SkipChildren
	case blackfriday.Code:
		out(w, codespanTag)
		escapeSpecialChars(w, node.Literal)
		out(w, codespanCloseTag)
	case blackfriday.Document:
		break
	case blackfriday.Paragraph:
		// roff .PP markers break lists
		if r.inList {
			return blackfriday.GoToNext
		}
		if entering {
			out(w, paraTag)
		} else {
			out(w, crTag)
		}
	case blackfriday.BlockQuote:
		if entering {
			out(w, quoteTag)
		} else {
			out(w, quoteCloseTag)
		}
	case blackfriday.Heading:
		if entering {
			switch node.Level {
			case 1:
				if r.firstHeader == false {
					out(w, titleHeader)
					r.firstHeader = true
					break
				}
				out(w, topLevelHeader)
			case 2:
				out(w, secondLevelHdr)
			default:
				out(w, otherHeader)
			}
		}
	case blackfriday.HorizontalRule:
		out(w, hruleTag)
	case blackfriday.List:
		openTag := listTag
		closeTag := listCloseTag
		if node.ListFlags&blackfriday.ListTypeDefinition != 0 {
			// tags for definition lists handled within Item node
			openTag = ""
			closeTag = ""
		}
		if entering {
			r.inList = true
			if node.ListFlags&blackfriday.ListTypeOrdered != 0 {
				r.ListCounters = append(r.ListCounters, 1)
			}
			out(w, openTag)
		} else {
			if node.ListFlags&blackfriday.ListTypeOrdered != 0 {
				r.ListCounters = r.ListCounters[:len(r.ListCounters)-1]
			}
			out(w, closeTag)
			r.inList = false
		}
	case blackfriday.Item:
		if entering {
			if node.ListFlags&blackfriday.ListTypeOrdered != 0 {
				out(w, fmt.Sprintf(".IP \"%3d.\" 5\n", r.ListCounters[len(r.ListCounters)-1]))
				r.ListCounters[len(r.ListCounters)-1]++
			} else if node.ListFlags&blackfriday.ListTypeDefinition != 0 {
				// state machine for handling terms and following definitions
				// since blackfriday does not distinguish them properly, nor
				// does it seperate them into separate lists as it should
				if r.defineTerm == false {
					out(w, arglistTag)
					r.defineTerm = true
				} else {
					r.defineTerm = false
				}
			} else {
				out(w, ".IP \\(bu 2\n")
			}
		} else {
			out(w, "\n")
		}
	case blackfriday.CodeBlock:
		out(w, codeTag)
		escapeSpecialChars(w, node.Literal)
		out(w, codeCloseTag)
	case blackfriday.Table:
		if entering {
			out(w, tableStart)
			//call walker to count cells (and rows?) so format section can be produced
			columns := countColumns(node)
			out(w, strings.Repeat("l ", columns)+"\n")
			out(w, strings.Repeat("l ", columns)+".\n")
		} else {
			out(w, tableEnd)
		}
	case blackfriday.TableCell:
		var (
			start, end string
		)
		if node.IsHeader {
			start = codespanTag
			end = codespanCloseTag
		}
		if entering {
			if node.Prev.Type == blackfriday.TableCell {
				out(w, "\t"+start)
			}
		} else {
			out(w, end)
		}
	case blackfriday.TableHead:
	case blackfriday.TableBody:
		// no action as cell entries do all the nroff formatting
		return blackfriday.GoToNext
	case blackfriday.TableRow:
		out(w, "\n")
	default:
		fmt.Fprintf(os.Stderr, "WARNING: go-md2man does not handle node type "+node.Type.String())
	}

	return blackfriday.GoToNext
}

// because roff format requires knowing the column count before outputting any table
// data we need to walk a table tree and count the columns
func countColumns(node *blackfriday.Node) int {
	var columns int

	node.Walk(func(node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
		switch node.Type {
		case blackfriday.TableRow:
			if !entering {
				return blackfriday.Terminate
			}
		case blackfriday.TableCell:
			columns++
		default:
			return blackfriday.GoToNext
		}
		return blackfriday.Terminate
	})
	return columns
}

func out(w io.Writer, output string) {
	io.WriteString(w, output)
}

func needsBackslash(c byte) bool {
	for _, r := range []byte("-_&\\~") {
		if c == r {
			return true
		}
	}
	return false
}

func escapeSpecialChars(w io.Writer, text []byte) {
	for i := 0; i < len(text); i++ {
		// escape initial apostrophe or period
		if len(text) >= 1 && (text[0] == '\'' || text[0] == '.') {
			io.WriteString(w, "\\&")
		}

		// directly copy normal characters
		org := i

		for i < len(text) && !needsBackslash(text[i]) {
			i++
		}
		if i > org {
			w.Write(text[org:i])
		}

		// escape a character
		if i >= len(text) {
			break
		}

		w.Write([]byte{'\\'})
		w.Write([]byte{text[i]})
	}
}
