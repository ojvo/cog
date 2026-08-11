package csv

import (
	"bytes"
	"testing"
	"time"
)

type Student struct {
	Id       int64
	Name     string
	Age      int
	Lat, Lng float64
	Birthday time.Time
}

func TestContainingHeader(t *testing.T) {
	data := bytes.NewBuffer([]byte(`id,name,age,lat,lng
201601101716,Will,18,40.654321,116.25820398331
201601101717,Jack,50,40.08296,116.316081
201601101718,Tony,44,40.060394,116.239552`))

	reader := NewReader[Student](data, ',', nil)
	students, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(students) != 3 {
		t.Fatalf("expected 3 students, got %d", len(students))
	}

	will := students[0]
	if will.Name != "Will" {
		t.Errorf("expected name 'Will', got '%s'", will.Name)
	}
	if will.Age != 18 {
		t.Errorf("expected age 18, got %d", will.Age)
	}
	if will.Id != 201601101716 {
		t.Errorf("expected id 201601101716, got %d", will.Id)
	}
}

func TestUsingHeaders(t *testing.T) {
	data := bytes.NewBuffer([]byte(`Will,201601101716,18,40.654321,116.25820398331,2016-01-10 23:59:59
Jack,201601101717,50,40.08296,116.316081,1990-01-31 23:59:59
Tony,201601101718,44,40.060394,116.239552,1963-10-10 23:59:59`))

	columns := []string{"name", "id", "age", "lat", "lng", "birthday"}
	reader := NewReader[Student](data, ',', columns)
	students, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(students) != 3 {
		t.Fatalf("expected 3 students, got %d", len(students))
	}

	if students[0].Name != "Will" {
		t.Errorf("expected name 'Will', got '%s'", students[0].Name)
	}

	if students[0].Birthday.Year() != 2016 {
		t.Errorf("expected year 2016, got %d", students[0].Birthday.Year())
	}
}

func TestIterator(t *testing.T) {
	data := bytes.NewBuffer([]byte(`id,name,age
1,Alice,30
2,Bob,25`))

	reader := NewReader[Student](data, ',', nil)
	it := reader.Iterator()

	count := 0
	for it.Next() {
		count++
		s := it.Row()
		if s.Name == "" {
			t.Error("expected name")
		}
	}
	if it.Err() != nil {
		t.Fatal(it.Err())
	}
	if count != 2 {
		t.Errorf("expected 2 records, got %d", count)
	}
}

func TestCustomTimeLayout(t *testing.T) {
	data := bytes.NewBuffer([]byte(`name,birthday
Alice,2006-01-02
Bob,1990-12-25`))

	type Person struct {
		Name     string
		Birthday time.Time
	}

	reader := NewReader[Person](data, ',', nil, WithTimeLayout[Person]("2006-01-02"))
	people, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(people) != 2 {
		t.Fatalf("expected 2 people, got %d", len(people))
	}

	if people[0].Birthday.Year() != 2006 {
		t.Errorf("expected year 2006, got %d", people[0].Birthday.Year())
	}
	if people[1].Birthday.Year() != 1990 {
		t.Errorf("expected year 1990, got %d", people[1].Birthday.Year())
	}
}

func TestReader_BoolField(t *testing.T) {
	data := bytes.NewBuffer([]byte(`name,active
Alice,true
Bob,false`))

	type User struct {
		Name   string
		Active bool
	}

	reader := NewReader[User](data, ',', nil)
	users, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	if !users[0].Active {
		t.Error("expected Alice active=true")
	}
	if users[1].Active {
		t.Error("expected Bob active=false")
	}
}

func TestReader_PointerType(t *testing.T) {
	data := bytes.NewBuffer([]byte(`name,age
Alice,30`))

	type User struct {
		Name string
		Age  int
	}

	reader := NewReader[*User](data, ',', nil)
	users, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0] == nil {
		t.Fatal("expected non-nil pointer")
	}
	if users[0].Name != "Alice" || users[0].Age != 30 {
		t.Errorf("expected Alice/30, got %s/%d", users[0].Name, users[0].Age)
	}
}

func TestReader_EmptyCell(t *testing.T) {
	data := bytes.NewBuffer([]byte(`name,age
Alice,
Bob,25`))

	type User struct {
		Name string
		Age  int
	}

	reader := NewReader[User](data, ',', nil)
	users, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	if users[0].Age != 0 {
		t.Errorf("expected age 0 for empty cell, got %d", users[0].Age)
	}
}

func TestReader_ParseError(t *testing.T) {
	data := bytes.NewBuffer([]byte(`name,age
Alice,notanumber`))

	type User struct {
		Name string
		Age  int
	}

	reader := NewReader[User](data, ',', nil)
	_, err := reader.ReadAll()
	if err == nil {
		t.Error("expected parse error for non-integer age")
	}
}
