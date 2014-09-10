package util

type ErrBatch struct {
	Err error
}

func (b *ErrBatch) Do(f func() error) {
	if b.Err != nil {
		return
	}
	err := f()
	if err != nil {
		b.Err = err
	}
}
