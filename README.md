# pesho

Named after St. Peter, the gatekeeper of the physical premises of initlab.org

The rest of this page is in Bulgarian and should not be read by anyone.

## Жици

Схемата включва:
 * **RaspberryPi B+**  на което работи демона за управление, свързано
   през етернет кабел за кораба-майка;
 * **DC Dual Motor Driver 30V 4A** -- драйвер за мотора в бравата,
     [линк](http://www.microbot.it/en/product/73/DC-Dual-Motor-Driver-30V-4A-V2.html)
 * **DC-DC Step-down** за да дадем 5V на малинката и
 * няколко оптрона и други пасивни компоненти.

[Схемата](https://raw.githubusercontent.com/kzyapkov/pesho/master/hardware/pesho.pdf),
начертана с KiCAD.

Накратко, Пешо чете 3 датчика на вратата:
 * "шиповете са извадени", на бравата BLK
 * "шиповете са прибрани", на бравата BUK
 * "вратата е затворена", на бравата BD
Пешо отключва и заключва бравата, като задвижва мотора за 200ms
в желаната посока.

## Код

На Go. Идеята е бравата да може да се манипулира чрез бутоните
и през HTTP. За тестове и дебъгване, HUP сигнал към процеса на
демона също ще отключи или заключи бравата.

## Строене

За успешен билд ти трябва Go Workspace, ето [как](https://golang.org/doc/code.html). После трябва
да упоменеш, че крос-компилираш за arm6:

    # tell go we're cross-compiling for arm6 (rpi):
    export GOOS=linux
    export GOARCH=arm
    export GOARM=6

и накрая, от тази директория:

    pwd
        ...github.com/kzyapkov/pesho
    go build .
    ls -al ./pesho

така получения `pesho` е статично линкнатото bin-че което седи
в /root/pesho и се пуска със `systemctl start pesho`.

## Следва

* Да следим захранването.

## License

      Copyright (c) 2014, initLab <vloo@initlab.org>
      All rights reserved.

      Redistribution and use in source and binary forms, with or without
      modification, are permitted provided that the following conditions are met:

      1. Redistributions of source code must retain the above copyright notice, this
         list of conditions and the following disclaimer.
      2. Redistributions in binary form must reproduce the above copyright notice,
         this list of conditions and the following disclaimer in the documentation
         and/or other materials provided with the distribution.

      THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
      ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
      WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
      DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
      ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
      (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
      LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
      ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
      (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
      SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

      The views and conclusions contained in the software and documentation are those
      of the authors and should not be interpreted as representing official policies,
      either expressed or implied, of the FreeBSD Project.
