package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"gobot.io/x/gobot"
	"gobot.io/x/gobot/drivers/gpio"
	"gobot.io/x/gobot/platforms/raspi"
)

type arrayFlags []string

func (i *arrayFlags) String() string {
	return "my string representation"
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

type Payment struct {
	Type  string
	Value int
}

func main() {
	var candyEndpoints arrayFlags
	flag.Var(&candyEndpoints, "lightning.subscription", "subscription endpoint to paid lightning invoices")
	var bitcoinAddress = flag.String("bitcoin.address", "", "receiving Bitcoin address")
	var initialDispense = flag.Duration("debug.dispense", 0, "dispensing duration on startup")

	flag.Parse()
	log.SetFlags(0)

	done := make(chan struct{})
	transactions := make(chan UtxMessage)
	invoices := make(chan Invoice)
	stop := make(chan bool)

	if *bitcoinAddress != "" {
		go listenForBlockchainTxns(*bitcoinAddress, transactions)
	}

	for _, endpoint := range candyEndpoints {
		go listenForCandyPayments(endpoint, invoices, stop)
	}

	r := raspi.NewAdaptor()
	motorPin := gpio.NewDirectPinDriver(r, "13")
	vibratorPin := gpio.NewDirectPinDriver(r, "11")
	touchSensor := gpio.NewButtonDriver(r, "7")

	work := func() {
		touchSensor.On(gpio.ButtonPush, func(data interface{}) {
			fmt.Println("button pressed")
		})

		touchSensor.On(gpio.ButtonRelease, func(data interface{}) {
			fmt.Println("button released")
		})

		if *initialDispense > 0 {
			fmt.Println("Initial dispensing is on")
			fmt.Println("Dispensing for", *initialDispense)

			motorPin.On()
			time.Sleep(*initialDispense)
			motorPin.Off()
		}

		for {
			var payment Payment

			select {
			case tx := <-transactions:
				value := 0

				for _, out := range tx.X.Out {
					if out.Addr == *bitcoinAddress {
						value += out.Value
					}
				}
				payment = Payment{Value: value, Type: "bitcoin"}
				log.Println("Payment is sent. ", payment)

				//				dispense := time.Duration(payment.Value/2) * time.Millisecond

				client := &http.Client{}
				//1 cents to BTC
				req, _ := http.NewRequest("GET", "https://blockchain.info/tobtc?currency=USD&value=0.01", nil)
				resp, err := client.Do(req)
				if err != nil {
					fmt.Println("Errored when sending request to the server")
					return
				}
				defer resp.Body.Close()
				respbody, _ := ioutil.ReadAll(resp.Body)
				fmt.Println(resp.Status)
				fmt.Println(string(respbody))
				v := string(respbody)
				//	value := 0.0001 //40 cents in BTC
				//				value := 10000.0 //40 cents in satoshi
				dispense := *initialDispense
				if s, err := strconv.ParseFloat(v, 32); err == nil {
					fmt.Printf("%T, %v\n", s, s)
					fmt.Println(float64(value) / math.Pow10(8) / s / 40) // 40cent = 10 M&M
					howMany := float64(value) / math.Pow10(8) / s / 40
					dispense = *initialDispense * time.Duration(howMany)
				}

				log.Println("Dispensing for a duration of", dispense)

				motorPin.On()
				time.Sleep(dispense)
				//				time.Sleep(*initialDispense)
				motorPin.Off()
			case invoice := <-invoices:
				payment = Payment{Value: invoice.Value, Type: "lightning"}
				log.Println("Payment is sent. ", payment)

				//				dispense := time.Duration(payment.Value/2) * time.Millisecond

				log.Println("Dispensing for a duration of", *initialDispense)

				motorPin.On()
				//				time.Sleep(dispense)
				time.Sleep(*initialDispense)
				motorPin.Off()
			}
		}
	}

	robot := gobot.NewRobot("bot",
		[]gobot.Connection{r},
		[]gobot.Device{motorPin, vibratorPin, touchSensor},
		work,
	)

	robot.Start()

	<-done
}
