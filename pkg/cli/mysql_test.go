package cli

import "testing"

func TestSQLTerminator(t *testing.T){
    cases:=map[string]bool{"select 1;":true,"select ';'":false,"select ';'; -- comment":true,"select 1 /* ; */":false,"select 'unfinished;":false,"select 1; /* unfinished":false,"select `a;b`;":true,"select 1 # ;":false}
    for input,want:=range cases{if got:=sqlComplete(input);got!=want{t.Errorf("sqlComplete(%q)=%v, want %v",input,got,want)}}
}
