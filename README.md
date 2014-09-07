# pesho

Named after St. Peter, the gatekeeper of the physical premises of initlab.org

The rest of this page is in Bulgarian and should not be read by anyone.

## Жици

Пешо чете 3 датчика на вратата:
 * "шиповете са извадени", на бравата BLK, в кода "locked"
 * "шиповете са прибрани", на бравата BUK, в кода "unlocked"
 * "вратата е затворена", на бравата BD, в кода "door"

Пешо цъка 3 релета. 2 от тях определят в каква посока ще се движат шиповете,
третото затваря веригата. То е там, за да може с акумулатор и тайни жици да
се манипулира вратата в случай на нужда.

На платката с релетата има 2x5 конектор за лентов кабел, pinout и връзване към RPi:

| GPIO   |     бележки       |     |        | бележки                | GPIO |
|--------|-------------------|-----|--------|------------------------|------|
|        | обща маса         | GND | 3.3V   | от RPi, за оптроните   |      |
|        |                   | N/C | N/C    |                        |      |
| 25     | датчик "unlocked" | BUK | LOCK   | посока заключване      | 10   |
| 8      | датчик "door"     | BD  | EN     | задвижва мотора        | 9    |
| 7      | датчик "locked"   | BLK | UNLOCK | посока отключване      | 11   |

Закачени са и два големи бутона на кутията -- за отваряне и затваряне.
Те замасяват GPIO0 и GPIO1. (щото там вече има pull-up резистори, пък
на платката не остана много място).

## Какво остава

### По-хардуерно

Според схемата в книжката на вратата, датчиците за отключено и заключено са
два отделни ключа. Реално, обаче, са един единствен ключ, тоест можем да минем
само с един датчик за следенето на шиповете. Обаче, цялата имплементирана вече
логика по управлението става малко излишна.

Пак според спецификацията, преместването на шиповете отнема 200ms. Реално се
случва за под 40ms. Това, в комбинация с горния факт налага пренаписване на
обработката на сигнали от GPIO сензорите.

Самите сенсори bounce-ват стабилно, и получаваме куп "интеръпти" (не са интеръпти
наистина, но изглеждат като такива.) Т.е. трябва debounce в софтуера, щото не ми
се играе с поялник повече, пък и пробвах с кондензатори тук-там -- ефекта
е незадоволителен.

### По-софтуерно

 * уеб частта
 * сигналите от бутоните на кутията
 * добавка на TLS (трябва да се копне от някой пример)

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
