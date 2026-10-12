package files

import (
	protocol "github.com/Ryo-del/Syne-protocol"
)

// doShareGroup открывает файл участникам группы (просмотр, скачивание, копирование).
// Править права может только владелец файла или преподаватель; отправитель должен быть в группе.
// Права «только контактам» на этом файле заменяются выбранными пользователями.
func (s *Service) doShareGroup(actor Actor, req protocol.FilesRequest) (protocol.FilesResponse, error) {
	t, err := s.open(actor, req.Owner, req.Path)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if t.rel == "" || t.info.IsDir() {
		return protocol.FilesResponse{}, invalid("only files can be shared with a group")
	}
	if !canManage(actor, t.owner.Login) {
		return protocol.FilesResponse{}, forbidden()
	}

	name, err := CleanName(req.Group)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	g, ok, err := s.st.GroupByName(name)
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	if !ok {
		return protocol.FilesResponse{}, notFound()
	}
	if !actor.IsTeacher() {
		me, found, err := s.st.UserByLogin(actor.Login)
		if err != nil {
			return protocol.FilesResponse{}, err
		}
		if !found || me.Group == "" || Fold(me.Group) != Fold(g.Name) {
			return protocol.FilesResponse{}, forbidden()
		}
	}

	users, err := s.st.AllUsers()
	if err != nil {
		return protocol.FilesResponse{}, err
	}
	var members []string
	for _, u := range users {
		if u.Group != "" && Fold(u.Group) == Fold(g.Name) && u.Login != t.owner.Login {
			members = append(members, u.Login)
		}
	}
	if len(members) == 0 {
		return protocol.FilesResponse{}, nil
	}

	unlock := s.lock(t.owner.Login)
	defer unlock()
	for _, a := range []protocol.Action{protocol.ActionView, protocol.ActionDownload, protocol.ActionCopy} {
		eff, err := t.eng.Effective(t.owner.Login, t.rel, a)
		if err != nil {
			return protocol.FilesResponse{}, err
		}
		list := members
		if eff.Found {
			switch eff.Rule.Mode {
			case protocol.ModeAll:
				continue // уже открыто всем
			case protocol.ModeSelected:
				list = append(append([]string{}, eff.Rule.Users...), members...)
			}
		}
		if err := s.eng.SetRule(actor, t.owner.Login, t.rel, false, a, protocol.ModeSelected, list); err != nil {
			return protocol.FilesResponse{}, err
		}
	}
	return protocol.FilesResponse{}, nil
}
