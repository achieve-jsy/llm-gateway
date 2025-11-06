package service

import (
	"errors"

	"github.com/example/llm-gateway/internal/auth/model"
	"github.com/example/llm-gateway/internal/auth/repository"
	"gorm.io/gorm"
)

type UserService struct {
	users *repository.UserRepository
	depts *repository.DeptRepository
}

func NewUserService(users *repository.UserRepository, depts *repository.DeptRepository) *UserService {
	return &UserService{users: users, depts: depts}
}

func (s *UserService) UpsertUserAndDept(sub, name, email, role, extDeptID, extDeptName string) (*model.User, error) {
	var deptRef *model.Department
	if extDeptID != "" {
		dept, err := s.depts.FindByDeptID(extDeptID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				dept = &model.Department{DeptID: extDeptID, DeptName: extDeptName}
				if err := s.depts.Create(dept); err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		}
		deptRef = dept
	}

	user, err := s.users.FindBySub(sub)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			created := &model.User{Sub: sub, Name: name, Email: email, Role: role, Source: "oidc"}
			if deptRef != nil {
				created.DeptID = &deptRef.ID
			}
			if err := s.users.Create(created); err != nil {
				return nil, err
			}
			return created, nil
		}
		return nil, err
	}
	user.Name = name
	user.Email = email
	user.Role = role
	if deptRef != nil {
		user.DeptID = &deptRef.ID
		} else {
		user.DeptID = nil
	}
	if err := s.users.Update(user); err != nil {
		return nil, err
	}
	return user, nil
}
