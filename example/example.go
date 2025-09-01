// Package example demonstrates common Go patterns with documentation
package example

import "fmt"

// Person represents an individual with basic information.
// It demonstrates a simple struct with various field types.
type Person struct {
	// Name is the person's full name
	Name string

	// Age represents the person's age in years
	Age int

	// Email is the person's contact email address
	Email string
}

// Greeter defines an interface for types that can provide greetings.
// This demonstrates a simple Go interface definition.
type Greeter interface {
	// Greet returns a greeting message
	Greet() string
}

// NewPerson creates and returns a new Person instance.
// This demonstrates a constructor function pattern in Go.
func NewPerson(name string, age int, email string) *Person {
	return &Person{
		Name:  name,
		Age:   age,
		Email: email,
	}
}

// Greet implements the Greeter interface for Person.
// It returns a friendly greeting using the person's name.
func (p *Person) Greet() string {
	return fmt.Sprintf("Hello, my name is %s!", p.Name)
}

// UpdateEmail updates the person's email address and returns any error.
// This demonstrates error handling in Go.
func (p *Person) UpdateEmail(newEmail string) error {
	if newEmail == "" {
		return fmt.Errorf("email cannot be empty")
	}
	p.Email = newEmail
	return nil
}

// HasBirthday increments the person's age by one year.
// This demonstrates a method that modifies the receiver.
func (p *Person) HasBirthday() {
	p.Age++
}

// PrintGreeting demonstrates using the Greeter interface.
// This shows how interfaces enable polymorphic behavior.
func PrintGreeting(g Greeter) {
	fmt.Println(g.Greet())
}
