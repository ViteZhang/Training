package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/textbook"
)

func textbookSections(sections []rules.TextbookSection) []gen.TextbookRevisionSection {
	out := make([]gen.TextbookRevisionSection, len(sections))
	for i, s := range sections {
		out[i] = gen.TextbookRevisionSection{Heading: s.Heading, Text: s.Text, SourcePage: s.SourcePage, PrintedPage: s.PrintedPage}
	}
	return out
}
func textbookRevision(r textbook.Revision) gen.TextbookRevision {
	return gen.TextbookRevision{Id: int64(r.ID), EditionId: int64(r.EditionID), RevisionNo: int64(r.RevisionNo), Version: int64(r.Version), Status: gen.TextbookRevisionStatus(r.Status), ContentHash: r.ContentHash, IndexStatus: gen.TextbookRevisionIndexStatus(r.IndexStatus), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Sections: textbookSections(r.Sections)}
}
func textbookSummary(r dbq.ListTextbookRevisionsRow) gen.TextbookRevisionSummary {
	return gen.TextbookRevisionSummary{Id: int64(r.ID), EditionId: int64(r.EditionID), RevisionNo: int64(r.RevisionNo), Version: int64(r.Version), Status: gen.TextbookRevisionSummaryStatus(r.Status), ContentHash: r.ContentHash, IndexStatus: gen.TextbookRevisionSummaryIndexStatus(r.IndexStatus), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}
func textbookBook(b textbook.Book) gen.Textbook {
	out := gen.Textbook{Id: int64(b.ID), Title: b.Title, Author: b.Author, Kind: gen.TextbookKind(b.Kind), Discipline: b.Discipline, Aliases: b.Aliases, Version: int64(b.Version), CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, Editions: []gen.TextbookEdition{}}
	for _, e := range b.Editions {
		edition := gen.TextbookEdition{Id: int64(e.ID), BookId: int64(e.BookID), Edition: e.Edition, Publisher: e.Publisher, Isbn: e.Isbn, LicenseNote: e.LicenseNote, Authorized: e.Authorized, ScopeNote: e.ScopeNote, Status: gen.TextbookEditionStatus(e.Status), Revisions: []gen.TextbookRevisionSummary{}}
		if e.PublicationYear.Valid {
			year := int(e.PublicationYear.Int16)
			edition.PublicationYear = &year
		}
		if e.CurrentRevisionID.Valid {
			edition.CurrentRevisionId = &e.CurrentRevisionID.Int64
		}
		for _, r := range e.Revisions {
			edition.Revisions = append(edition.Revisions, textbookSummary(r))
		}
		out.Editions = append(out.Editions, edition)
	}
	return out
}
func editionInput(e gen.TextbookEditionInput) rules.TextbookEditionInput {
	return rules.TextbookEditionInput{Edition: e.Edition, Publisher: deref(e.Publisher), PublicationYear: e.PublicationYear, ISBN: deref(e.Isbn), LicenseNote: e.LicenseNote, Authorized: e.Authorized, ScopeNote: deref(e.ScopeNote)}
}
func (h *Handlers) ListTextbooks(c *gin.Context, params gen.ListTextbooksParams) {
	page, size := 1, 20
	if params.Page != nil {
		page = *params.Page
	}
	if params.PageSize != nil {
		size = *params.PageSize
	}
	books, total, err := h.deps.Textbooks.List(c.Request.Context(), textbook.Filter{Search: deref(params.Q), Kind: deref(params.Kind), Discipline: deref(params.Discipline), Page: page, PageSize: size})
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.Textbook, len(books))
	for i, b := range books {
		items[i] = textbookBook(b)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}
func (h *Handlers) GetTextbook(c *gin.Context, bookID int64) {
	b, err := h.deps.Textbooks.Get(c.Request.Context(), uint64(bookID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookBook(b))
}
func (h *Handlers) CreateTextbook(c *gin.Context) {
	var body gen.TextbookCreate
	if !bind(c, &body) {
		return
	}
	b, err := h.deps.Textbooks.Create(c.Request.Context(), currentAdmin(c).ID, body.IdempotencyKey, rules.TextbookMetadata{Title: body.Title, Author: body.Author, Kind: string(body.Kind), Discipline: body.Discipline, Aliases: body.Aliases}, editionInput(body.InitialEdition))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookBook(b))
}
func (h *Handlers) UpdateTextbook(c *gin.Context, bookID int64) {
	var body gen.TextbookUpdate
	if !bind(c, &body) {
		return
	}
	b, err := h.deps.Textbooks.Update(c.Request.Context(), uint64(bookID), uint64(body.ExpectedVersion), rules.TextbookMetadata{Title: body.Title, Author: body.Author, Kind: string(body.Kind), Discipline: body.Discipline, Aliases: body.Aliases})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookBook(b))
}
func (h *Handlers) CreateTextbookEdition(c *gin.Context, bookID int64) {
	var body gen.TextbookEditionCreate
	if !bind(c, &body) {
		return
	}
	b, err := h.deps.Textbooks.AddEdition(c.Request.Context(), uint64(bookID), currentAdmin(c).ID, body.IdempotencyKey, editionInput(gen.TextbookEditionInput{Edition: body.Edition, Publisher: body.Publisher, PublicationYear: body.PublicationYear, Isbn: body.Isbn, LicenseNote: body.LicenseNote, Authorized: body.Authorized, ScopeNote: body.ScopeNote}))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookBook(b))
}
func (h *Handlers) GetTextbookRevision(c *gin.Context, bookID, editionID, revisionID int64) {
	r, err := h.deps.Textbooks.GetRevision(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookRevision(r))
}
func (h *Handlers) CreateTextbookRevision(c *gin.Context, bookID, editionID int64) {
	var body gen.TextbookRevisionCreate
	if !bind(c, &body) {
		return
	}
	r, err := h.deps.Textbooks.NewRevision(c.Request.Context(), uint64(bookID), uint64(editionID), currentAdmin(c).ID, body.IdempotencyKey)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookRevision(r))
}
func (h *Handlers) SaveTextbookRevision(c *gin.Context, bookID, editionID, revisionID int64) {
	var body gen.TextbookRevisionSave
	if !bind(c, &body) {
		return
	}
	sections := make([]rules.TextbookSection, len(body.Sections))
	for i, s := range body.Sections {
		sections[i] = rules.TextbookSection{Heading: s.Heading, Text: s.Text, SourcePage: s.SourcePage, PrintedPage: s.PrintedPage}
	}
	r, err := h.deps.Textbooks.SaveRevision(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID), currentAdmin(c).ID, uint64(body.ExpectedVersion), sections, body.MarkReviewed)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookRevision(r))
}
