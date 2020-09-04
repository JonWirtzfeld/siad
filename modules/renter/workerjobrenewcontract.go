package renter

import (
	"context"

	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/modules/renter/proto"

	"gitlab.com/NebulousLabs/errors"
)

type (
	// jobRenew contains information about a Renew query.
	jobRenew struct {
		staticResponseChan       chan *jobRenewResponse // Channel to send a response down
		staticTransactionBuilder modules.TransactionBuilder
		staticParams             proto.ContractParams

		*jobGeneric
	}

	// jobReadQueue is a list of Renew queries that have been assigned to the
	// worker.
	jobRenewQueue struct {
		*jobGenericQueue
	}

	// jobReadResponse contains the result of a Renew query.
	jobRenewResponse struct {
		staticErr error

		// The worker is included in the response so that the caller can listen
		// on one channel for a bunch of workers and still know which worker
		// successfully found the sector root.
		staticWorker *worker
	}
)

// renewJobExpectedBandwidth is a helper function that returns the expected
// bandwidth consumption of a renew job.
func renewJobExpectedBandwidth() (ul, dl uint64) {
	ul = 1 << 13 // 8 KiB
	dl = 1 << 13 // 8 KiB
	return
}

// callDiscard will discard a job, sending the provided error.
func (j *jobRenew) callDiscard(err error) {
	w := j.staticQueue.staticWorker()
	w.renter.tg.Launch(func() {
		response := &jobRenewResponse{
			staticErr: errors.Extend(err, ErrJobDiscarded),
		}
		select {
		case j.staticResponseChan <- response:
		case <-j.staticCtx.Done():
		case <-w.renter.tg.StopChan():
		}
	})
}

// callExecute will run the renew job.
func (j *jobRenew) callExecute() {
	w := j.staticQueue.staticWorker()
	err := w.managedRenew(j.staticParams, j.staticTransactionBuilder)

	// Send the response.
	response := &jobRenewResponse{
		staticErr: err,

		staticWorker: w,
	}
	w.renter.tg.Launch(func() {
		select {
		case j.staticResponseChan <- response:
		case <-j.staticCtx.Done():
		case <-w.renter.tg.StopChan():
		}
	})

	// Report success or failure to the queue.
	if err == nil {
		j.staticQueue.callReportSuccess()
	} else {
		j.staticQueue.callReportFailure(err)
		return
	}
}

// callExpectedBandwidth returns the amount of bandwidth this job is expected to
// consume.
func (j *jobRenew) callExpectedBandwidth() (ul, dl uint64) {
	return renewJobExpectedBandwidth()
}

// initJobRenewQueue will initialize a queue for renewing contracts with a host
// for the worker. This is only meant to be run once at startup.
func (w *worker) initJobRenewQueue() {
	// Sanity check that there is no existing job queue.
	if w.staticJobRenewQueue != nil {
		w.renter.log.Critical("incorret call on initJobRenewQueue")
		return
	}

	w.staticJobRenewQueue = &jobRenewQueue{
		jobGenericQueue: newJobGenericQueue(w),
	}
}

// RenewContract renews the contract with the worker's host.
func (w *worker) RenewContract(ctx context.Context, params proto.ContractParams, txnBuilder modules.TransactionBuilder) error {
	renewResponseChan := make(chan *jobRenewResponse)
	params.PriceTable = &w.staticPriceTable().staticPriceTable
	jro := &jobRenew{
		staticParams:             params,
		staticResponseChan:       renewResponseChan,
		staticTransactionBuilder: txnBuilder,
		jobGeneric:               newJobGeneric(ctx, w.staticJobReadQueue),
	}

	// Add the job to the queue.
	if !w.staticJobRenewQueue.callAdd(jro) {
		return errors.New("worker unavailable")
	}

	// Wait for the response.
	var resp *jobRenewResponse
	select {
	case <-ctx.Done():
		return errors.New("Renew interrupted")
	case resp = <-renewResponseChan:
	}
	return resp.staticErr
}
