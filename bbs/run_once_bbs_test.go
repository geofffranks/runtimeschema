package bbs_test

import (
	. "github.com/cloudfoundry-incubator/runtime-schema/bbs"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"
)

var _ = Describe("RunOnce BBS", func() {
	var bbs *BBS
	var runOnce models.RunOnce

	BeforeEach(func() {
		bbs = New(store)
		runOnce = models.RunOnce{
			Guid:            "some-guid",
			ExecutorID:      "executor-id",
			ContainerHandle: "container-handle",
		}
	})

	itRetriesUntilStoreComesBack := func(action func(*BBS, models.RunOnce) error) {
		It("should keep trying until the store comes back", func(done Done) {
			etcdRunner.GoAway()

			runResult := make(chan error)
			go func() {
				err := action(bbs, runOnce)
				runResult <- err
			}()

			time.Sleep(200 * time.Millisecond)

			etcdRunner.ComeBack()

			Ω(<-runResult).ShouldNot(HaveOccurred())

			close(done)
		}, 5)
	}

	Describe("DesireRunOnce", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("creates /run_once/pending/<guid>", func() {
			node, err := store.Get("/v1/run_once/pending/some-guid")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node.Value).Should(Equal(runOnce.ToJSON()))
		})

		Context("when the RunOnce is already pending", func() {
			It("should happily overwrite the existing RunOnce", func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack((*BBS).DesireRunOnce)
		})
	})

	Describe("ResolveRunOnce", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should remove /run_once/pending/<guid>", func() {
			err := bbs.ResolveRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			_, err = store.Get("/v1/run_once/pending/some-guid")
			Ω(err).Should(Equal(storeadapter.ErrorKeyNotFound))
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack((*BBS).ResolveRunOnce)
		})
	})

	Describe("MaintainPresence", func() {
		var (
			executorId string
			interval   uint64
			stop       chan bool
			err        error
		)

		BeforeEach(func() {
			executorId = "stubExecutor"
			interval = uint64(1)

			stop, err = bbs.MaintainPresence(interval, executorId)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("should put /executor/EXECUTOR_ID in the store with a TTL", func() {
			node, err := store.Get("/v1/executor/" + executorId)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(Equal(storeadapter.StoreNode{
				Key:   "/v1/executor/" + executorId,
				Value: []byte{},
				TTL:   interval, // move to config one day
			}))

			close(stop)
		})

		It("should periodically maintain the TTL", func() {
			time.Sleep(2 * time.Second)

			_, err = store.Get("/v1/executor/" + executorId)
			Ω(err).ShouldNot(HaveOccurred())

			close(stop)
		})

		It("should be possible to stop maintaining presence", func() {
			close(stop)

			time.Sleep(2 * time.Second)

			_, err = store.Get("/v1/executor/" + executorId)
			Ω(err).Should(Equal(storeadapter.ErrorKeyNotFound))
		})
	})

	Describe("ClaimRunOnce", func() {
		Context("when claimed with a correctly configured runOnce", func() {
			BeforeEach(func() {
				runOnce.ExecutorID = "executor-id"
			})

			It("creates /run_once/claimed/<guid>", func() {
				err := bbs.ClaimRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				node, err := store.Get("/v1/run_once/claimed/some-guid")
				Ω(err).ShouldNot(HaveOccurred())
				Ω(node).Should(Equal(storeadapter.StoreNode{
					Key:   "/v1/run_once/claimed/some-guid",
					Value: runOnce.ToJSON(),
					TTL:   10, // move to config one day
				}))
			})
		})

		Context("when claimed with a runOnce that is missing ExecutorID", func() {
			BeforeEach(func() {
				runOnce.ExecutorID = ""
			})

			It("should panic", func() {
				Ω(func() {
					bbs.ClaimRunOnce(runOnce)
				}).Should(Panic())
			})
		})

		Context("when the RunOnce is already claimed", func() {
			BeforeEach(func() {
				err := bbs.ClaimRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns an error", func() {
				err := bbs.ClaimRunOnce(runOnce)
				Ω(err).Should(HaveOccurred())
			})
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack((*BBS).ClaimRunOnce)
		})
	})

	Describe("StartRunOnce", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("creates running", func() {
			err := bbs.StartRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := store.Get("/v1/run_once/running/some-guid")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(Equal(storeadapter.StoreNode{
				Key:   "/v1/run_once/running/some-guid",
				Value: runOnce.ToJSON(),
			}))
		})

		Context("when starting with a runOnce that is missing ExecutorID", func() {
			BeforeEach(func() {
				runOnce.ExecutorID = ""
			})

			It("should panic", func() {
				Ω(func() {
					bbs.StartRunOnce(runOnce)
				}).Should(Panic())
			})
		})

		Context("when starting with a runOnce that is missing ContainerHandle", func() {
			BeforeEach(func() {
				runOnce.ContainerHandle = ""
			})

			It("should panic", func() {
				Ω(func() {
					bbs.StartRunOnce(runOnce)
				}).Should(Panic())
			})
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack((*BBS).StartRunOnce)
		})
	})

	Describe("CompleteRunOnce", func() {
		BeforeEach(func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.ClaimRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.StartRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())
		})

		It("creates an entry under /run_once/completed", func() {
			runOnce.Failed = true
			runOnce.FailureReason = "because i said so"

			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			node, err := store.Get("/v1/run_once/completed/some-guid")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(node).Should(Equal(storeadapter.StoreNode{
				Key:   "/v1/run_once/completed/some-guid",
				Value: runOnce.ToJSON(),
			}))
		})

		Context("when the store is out of commission", func() {
			itRetriesUntilStoreComesBack((*BBS).CompleteRunOnce)
		})
	})

	Describe("WatchForDesiredRunOnce", func() {
		var (
			events <-chan (models.RunOnce)
			stop   chan<- bool
		)

		BeforeEach(func() {
			events, stop, _ = bbs.WatchForDesiredRunOnce()
		})

		It("should send an event down the pipe for creates", func(done Done) {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			close(done)
		})

		It("should send an event down the pipe for sets", func(done Done) {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			err = bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			close(done)
		})

		It("should not send an event down the pipe for deletes", func(done Done) {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			err = bbs.ResolveRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			otherRunOnce := runOnce
			otherRunOnce.Guid = runOnce.Guid + "1"

			err = bbs.DesireRunOnce(otherRunOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(otherRunOnce))

			close(done)
		})

		It("closes the events channel when told to stop", func(done Done) {
			stop <- true

			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			_, ok := <-events

			Expect(ok).To(BeFalse())

			close(done)
		})
	})

	Describe("WatchForCompletedRunOnce", func() {
		var (
			events <-chan (models.RunOnce)
			stop   chan<- bool
		)

		BeforeEach(func() {
			events, stop, _ = bbs.WatchForCompletedRunOnce()
		})

		It("should send an event down the pipe for creates", func(done Done) {
			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			close(done)
		})

		It("should send an event down the pipe for sets", func(done Done) {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			err = bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			bbs.ConvergeRunOnce() //should bump the completed key

			Expect(<-events).To(Equal(runOnce))

			close(done)
		})

		It("should not send an event down the pipe for deletes", func(done Done) {
			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(runOnce))

			bbs.ConvergeRunOnce() //should delete the key

			otherRunOnce := runOnce
			otherRunOnce.Guid = runOnce.Guid + "1"

			err = bbs.CompleteRunOnce(otherRunOnce)
			Ω(err).ShouldNot(HaveOccurred())

			Expect(<-events).To(Equal(otherRunOnce))

			close(done)
		})

		It("closes the events channel when told to stop", func(done Done) {
			stop <- true

			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			_, ok := <-events

			Expect(ok).To(BeFalse())

			close(done)
		})
	})

	Describe("GetAllPendingRunOnces", func() {
		It("returns all RunOnces in 'pending' state", func() {
			err := bbs.DesireRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			runOnces, err := bbs.GetAllPendingRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("GetAllClaimedRunOnces", func() {
		It("returns all RunOnces in 'claimed' state", func() {
			err := bbs.ClaimRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			runOnces, err := bbs.GetAllClaimedRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("GetAllStartingRunOnces", func() {
		It("returns all RunOnces in 'running' state", func() {
			err := bbs.StartRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			runOnces, err := bbs.GetAllStartingRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("GetAllCompletedRunOnces", func() {
		It("returns all RunOnces in 'completed' state", func() {
			err := bbs.CompleteRunOnce(runOnce)
			Ω(err).ShouldNot(HaveOccurred())

			runOnces, err := bbs.GetAllCompletedRunOnces()
			Ω(err).ShouldNot(HaveOccurred())
			Ω(runOnces).Should(HaveLen(1))
			Ω(runOnces).Should(ContainElement(runOnce))
		})
	})

	Describe("Locking", func() {
		It("grabs the lock and holds it", func() {
			ttl := 1 * time.Second
			result, _ := bbs.GrabRunOnceLock(ttl)
			Ω(result).To(BeTrue())

			result, _ = bbs.GrabRunOnceLock(ttl)
			Ω(result).To(BeFalse())
		})

	})

	Describe("ConvergeRunOnce", func() {
		var otherRunOnce models.RunOnce

		BeforeEach(func() {
			otherRunOnce = models.RunOnce{
				Guid: "some-other-guid",
			}
		})

		Context("when a pending key exists", func() {
			BeforeEach(func() {
				err := bbs.DesireRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			Context("and there is a claim key", func() {
				BeforeEach(func() {
					err := bbs.ClaimRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("should not kick the pending key", func(done Done) {
					events, _, _ := bbs.WatchForDesiredRunOnce()

					bbs.ConvergeRunOnce()

					bbs.DesireRunOnce(otherRunOnce)

					Ω(<-events).Should(Equal(otherRunOnce))

					close(done)
				})
			})

			Context("and there is a running key", func() {
				BeforeEach(func() {
					err := bbs.StartRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("should not kick the pending key", func(done Done) {
					events, _, _ := bbs.WatchForDesiredRunOnce()

					bbs.ConvergeRunOnce()

					bbs.DesireRunOnce(otherRunOnce)

					Ω(<-events).Should(Equal(otherRunOnce))

					close(done)
				})
			})

			Context("and there is a completed key", func() {
				BeforeEach(func() {
					err := bbs.CompleteRunOnce(runOnce)
					Ω(err).ShouldNot(HaveOccurred())
				})

				It("should not kick the pending key", func(done Done) {
					events, _, _ := bbs.WatchForDesiredRunOnce()

					bbs.ConvergeRunOnce()

					bbs.DesireRunOnce(otherRunOnce)

					Ω(<-events).Should(Equal(otherRunOnce))

					close(done)
				})

				It("should kick the completed key", func(done Done) {
					events, _, _ := bbs.WatchForCompletedRunOnce()

					bbs.ConvergeRunOnce()

					Ω(<-events).Should(Equal(runOnce))

					close(done)
				})
			})

			Context("and there are no other keys", func() {
				It("should kick the pending key",
					func(done Done) {
						events, _, _ := bbs.WatchForDesiredRunOnce()

						bbs.ConvergeRunOnce()

						Ω(<-events).Should(Equal(runOnce))

						close(done)
					})
			})
		})

		Context("when a pending key does not exist", func() {
			BeforeEach(func() {
				err := bbs.ClaimRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.StartRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())

				err = bbs.CompleteRunOnce(runOnce)
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("should delete any extra keys", func() {
				bbs.ConvergeRunOnce()

				_, err := store.Get("/v1/run_once/claimed/some-guid")
				Ω(err).Should(HaveOccurred())

				_, err = store.Get("/v1/run_once/running/some-guid")
				Ω(err).Should(HaveOccurred())

				_, err = store.Get("/v1/run_once/completed/some-guid")
				Ω(err).Should(HaveOccurred())
			})
		})
	})
})
