package graphql

import "github.com/overmindv/arcee/internal/usecase"

type Resolver struct{ Users *usecase.UserService }
