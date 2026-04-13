package model

type User struct {
	ID        int64  `json:"id" gorm:"column:id;primaryKey"`
	Username  string `json:"username" gorm:"column:username"`
	Password  string `json:"-" gorm:"column:password"`
	Avatar    string `json:"avatar,omitempty" gorm:"column:avatar"`
	Profile   string `json:"profile,omitempty" gorm:"column:profile"`
	CreatedAt int64  `json:"-" gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt int64  `json:"-" gorm:"column:updated_at;autoUpdateTime:milli"`
}

func (User) TableName() string {
	return "users"
}
